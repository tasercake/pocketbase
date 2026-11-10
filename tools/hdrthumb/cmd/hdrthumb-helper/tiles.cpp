#include "image_tools.h"
#include "srgb_icc.h"

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <iostream>
#include <limits>
#include <utility>

// Keep the same grid for both renditions and all neighboring tiles. An
// eight-pixel overlap aligns the JPEG 4:4:4 blocks on each side of the seam.
constexpr int kTileSize = 512;
constexpr int kTileOverlap = 8;
constexpr int kTilePreviewLimit = 2000;
constexpr size_t kMaxTilePlanePixels = 100000000;
constexpr char kISO[] = "urn:iso:std:iso:ts:21496:-1";
constexpr char kICC[] = "ICC_PROFILE";
using Bytes = std::vector<unsigned char>;

static uint32_t markerValue(const unsigned char* p, int size, bool little) {
  uint32_t result = 0;
  for (int i = 0; i < size; i++) {
    result = (result << 8) | p[little ? size - 1 - i : i];
  }
  return result;
}

static int exifOrientation(const uhdr_mem_block_t* exif) {
  if (!exif || exif->data_sz < 14 || std::memcmp(exif->data, "Exif\0\0", 6) != 0) return 1;
  const auto* tiff = static_cast<const unsigned char*>(exif->data) + 6;
  const size_t size = exif->data_sz - 6;
  const bool little = tiff[0] == 'I' && tiff[1] == 'I';
  if (!little && (tiff[0] != 'M' || tiff[1] != 'M')) return 1;
  if (markerValue(tiff + 2, 2, little) != 42) return 1;
  size_t offset = markerValue(tiff + 4, 4, little);
  if (offset > size - 2) return 1;
  const auto entries = markerValue(tiff + offset, 2, little);
  offset += 2;
  for (uint32_t i = 0; i < entries && size - offset >= 12; i++, offset += 12) {
    const auto* entry = tiff + offset;
    if (markerValue(entry, 2, little) == 0x112 &&
        markerValue(entry + 2, 2, little) == 3 &&
        markerValue(entry + 4, 4, little) == 1) {
      const auto value = markerValue(entry + 8, 2, little);
      if (value >= 1 && value <= 8) return static_cast<int>(value);
    }
  }
  return 1;
}

static std::vector<Bytes> appBlocks(const uhdr_mem_block_t* jpeg, int markerType,
                                   const char* signature, size_t signatureSize) {
  const auto* bytes = static_cast<const unsigned char*>(jpeg->data);
  const size_t size = jpeg->data_sz;
  std::vector<Bytes> blocks;
  for (size_t pos = 2; pos < size && bytes[pos] == 0xff;) {
    do { pos++; } while (pos < size && bytes[pos] == 0xff);
    if (size - pos < 3 || bytes[pos] == 0xda || bytes[pos] == 0xd9) break;
    const int marker = bytes[pos++];
    const size_t length = markerValue(bytes + pos, 2, false);
    if (length < 2 || length > size - pos) break;
    if (marker == markerType && length >= 2 + signatureSize &&
        std::memcmp(bytes + pos + 2, signature, signatureSize) == 0) {
      blocks.emplace_back(bytes + pos + 2, bytes + pos + length);
    }
    pos += length;
  }
  return blocks;
}

static void putNumber(Bytes& out, uint32_t value, int width) {
  for (int i = width - 1; i >= 0; i--) {
    out.push_back(static_cast<unsigned char>(value >> (8 * i)));
  }
}

static void appendAPP2(Bytes& out, const Bytes& payload) {
  out.insert(out.end(), {0xff, 0xe2});
  putNumber(out, static_cast<uint32_t>(payload.size() + 2), 2);
  out.insert(out.end(), payload.begin(), payload.end());
}

static Bytes primaryISO() {
  Bytes result(kISO, kISO + sizeof(kISO));
  result.resize(result.size() + 4, 0);
  return result;
}

// Older masters have XMP-only gain metadata. Convert the values already
// validated by libultrahdr to ISO rationals, without applying a new tone curve.
static Bytes legacyGainISO(const uhdr_gainmap_metadata_t& meta) {
  Bytes result = primaryISO();
  result.push_back(meta.use_base_cg ? 0xc0 : 0x80);  // Three channels, separate denominators.
  auto fraction = [&](double value) {
    constexpr uint32_t denom = 100000000;
    const double numerator = std::round(value * denom);
    if (!std::isfinite(numerator) || numerator < std::numeric_limits<int32_t>::min() ||
        numerator > std::numeric_limits<int32_t>::max()) die("invalid legacy HDR metadata");
    putNumber(result, static_cast<uint32_t>(static_cast<int32_t>(numerator)), 4);
    putNumber(result, denom, 4);
  };
  fraction(std::log2(meta.hdr_capacity_min));
  fraction(std::log2(meta.hdr_capacity_max));
  for (int i = 0; i < 3; i++) {
    fraction(std::log2(meta.min_content_boost[i]));
    fraction(std::log2(meta.max_content_boost[i]));
    fraction(meta.gamma[i]);
    fraction(meta.offset_sdr[i]);
    fraction(meta.offset_hdr[i]);
  }
  return result;
}

static Bytes tileMPF(uint32_t primarySize, uint32_t gainSize, uint32_t gainOffset) {
  Bytes out{'M', 'P', 'F', 0, 'M', 'M', 0, 42};
  putNumber(out, 8, 4);  // Index IFD offset from the TIFF header.
  putNumber(out, 3, 2);
  const auto entry = [&](uint16_t tag, uint16_t type, uint32_t count, uint32_t value) {
    putNumber(out, tag, 2);
    putNumber(out, type, 2);
    putNumber(out, count, 4);
    putNumber(out, value, 4);
  };
  entry(0xb000, 7, 4, 0x30313030);  // MPF version "0100".
  entry(0xb001, 4, 1, 2);           // Two images.
  entry(0xb002, 7, 32, 50);         // Two 16-byte MP entries at TIFF offset 50.
  putNumber(out, 0, 4);             // No next IFD.
  putNumber(out, 0x20030000, 4);    // Representative primary image.
  putNumber(out, primarySize, 4);
  putNumber(out, 0, 4);
  putNumber(out, 0, 4);
  putNumber(out, 0, 4);
  putNumber(out, gainSize, 4);
  putNumber(out, gainOffset, 4);
  putNumber(out, 0, 4);
  return out;
}

static size_t jpegHeaderEnd(const Bytes& encoded) {
  // Retain libjpeg's leading JFIF block when present.
  if (encoded.size() >= 6 && encoded[2] == 0xff && encoded[3] == 0xe0) {
    return 4 + markerValue(encoded.data() + 4, 2, false);
  }
  return 2;
}

static Bytes tilePrefix(const Bytes& encoded, const std::vector<Bytes>& icc, const Bytes& iso) {
  Bytes prefix(encoded.begin(), encoded.begin() + jpegHeaderEnd(encoded));
  for (const auto& block : icc) appendAPP2(prefix, block);
  if (!iso.empty()) appendAPP2(prefix, iso);
  return prefix;
}

static void writeSDRTile(const std::string& path, const RGBImage& image, const std::vector<Bytes>& icc) {
  const auto encoded = encodeJpeg(image, false);
  auto output = tilePrefix(encoded, icc, {});
  output.insert(output.end(), encoded.begin() + jpegHeaderEnd(encoded), encoded.end());
  writeFile(path, output.data(), output.size());
}

static void writeHDRTile(const std::string& path, const RGBImage& base, const RGBImage& gain,
                         const std::vector<Bytes>& baseICC, const std::vector<Bytes>& gainICC,
                         const Bytes& gainISO) {
  const auto baseJPEG = encodeJpeg(base, false);
  const auto gainJPEG = encodeJpeg(gain, false);
  auto result = tilePrefix(baseJPEG, baseICC, primaryISO());
  auto secondary = tilePrefix(gainJPEG, gainICC, gainISO);
  secondary.insert(secondary.end(), gainJPEG.begin() + jpegHeaderEnd(gainJPEG), gainJPEG.end());

  // Place the MP index before DQT and calculate its offset from the TIFF
  // header. Some Image I/O decoders ignore the MP index after DQT.
  const uint32_t primarySize = result.size() + 90 + baseJPEG.size() - jpegHeaderEnd(baseJPEG);
  const uint32_t secondaryOffset = primarySize - (result.size() + 8);
  appendAPP2(result, tileMPF(primarySize, secondary.size(), secondaryOffset));
  result.insert(result.end(), baseJPEG.begin() + jpegHeaderEnd(baseJPEG), baseJPEG.end());
  result.insert(result.end(), secondary.begin(), secondary.end());
  writeFile(path, result.data(), result.size());
}

static const unsigned char* orientedPixel(const RGBImage& img, int orientation, int x, int y) {
  int sx = x, sy = y;
  if (orientation >= 5) std::swap(sx, sy);
  if (orientation == 2 || orientation == 3 || orientation == 7 || orientation == 8) sx = img.w - 1 - sx;
  if (orientation == 3 || orientation == 4 || orientation == 6 || orientation == 7) sy = img.h - 1 - sy;
  return &img.px[(static_cast<size_t>(sy) * img.w + sx) * 3];
}

struct TileSample {
  int start;
  int end;
  double left;
  double right;
  bool area;

  double weight(int p) const {
    if (start == end) return 1.0;
    if (!area) return p == start ? 1.0 - (left - start) : left - start;
    return (std::min(right, p + 1.0) - std::max(left, static_cast<double>(p))) / (right - left);
  }
};

static TileSample tileSample(int index, int srcSize, int dstSize) {
  if (srcSize > dstSize) {
    const double left = static_cast<double>(index) * srcSize / dstSize;
    const double right = static_cast<double>(index + 1) * srcSize / dstSize;
    return {static_cast<int>(left), std::min(srcSize - 1, static_cast<int>(std::ceil(right)) - 1), left, right, true};
  }
  const double x = std::clamp(sourceCoord(index, 0, srcSize, dstSize), 0.0, static_cast<double>(srcSize - 1));
  const int start = static_cast<int>(x);
  return {start, std::min(start + 1, srcSize - 1), x, x, false};
}

static RGBImage sampleTile(const RGBImage& src, int orientation, int levelW, int levelH,
                           int left, int top, int width, int height) {
  RGBImage output;
  output.w = width;
  output.h = height;
  output.grayscale = src.grayscale;
  output.px.resize(static_cast<size_t>(width) * height * 3);
  const int srcW = orientation >= 5 ? src.h : src.w;
  const int srcH = orientation >= 5 ? src.w : src.h;
  if (srcW == levelW && srcH == levelH) {
    for (int y = 0; y < height; y++) {
      auto* row = &output.px[static_cast<size_t>(y) * width * 3];
      if (orientation == 1) {
        std::memcpy(row, orientedPixel(src, orientation, left, top + y), width * 3);
      } else {
        for (int x = 0; x < width; x++) {
          std::memcpy(row + x * 3, orientedPixel(src, orientation, left + x, top + y), 3);
        }
      }
    }
    return output;
  }
  std::vector<TileSample> columns;
  columns.reserve(width);
  for (int x = 0; x < width; x++) columns.push_back(tileSample(left + x, srcW, levelW));
  for (int y = 0; y < height; y++) {
    auto* row = &output.px[static_cast<size_t>(y) * width * 3];
    const auto ys = tileSample(top + y, srcH, levelH);
    for (int x = 0; x < width; x++) {
      const auto& xs = columns[x];
      double rgb[3] = {};
      for (int sy = ys.start; sy <= ys.end; sy++) {
        const double wy = ys.weight(sy);
        for (int sx = xs.start; sx <= xs.end; sx++) {
          const double weight = wy * xs.weight(sx);
          const auto* pixel = orientedPixel(src, orientation, sx, sy);
          for (int k = 0; k < 3; k++) rgb[k] += weight * pixel[k];
        }
      }
      for (int k = 0; k < 3; k++) row[x * 3 + k] = static_cast<unsigned char>(rgb[k] + 0.5);
    }
  }
  return output;
}

void generateTiles(const std::string& input, const std::string& directory, bool hdr) {
  auto bytes = readFile(input);
  RGBImage base;
  RGBImage gain;
  int orientation = 1;
  std::vector<Bytes> baseICC;
  std::vector<Bytes> gainICC;
  Bytes gainISO;
  if (hdr) {
    auto img = comp(bytes);
    auto* dec = uhdr_create_decoder();
    if (!dec) die("decoder allocation failed");
    check(uhdr_dec_set_image(dec, &img), "set tile source");
    check(uhdr_dec_probe(dec), "probe tile source");

    const auto* baseBlock = uhdr_dec_get_base_image(dec);
    const auto* gainBlock = uhdr_dec_get_gainmap_image(dec);
    const auto* metadata = uhdr_dec_get_gainmap_metadata(dec);
    if (!baseBlock || !gainBlock || !metadata) die("missing tile source gain map");
    orientation = exifOrientation(uhdr_dec_get_exif(dec));
    baseICC = appBlocks(baseBlock, 0xe2, kICC, sizeof(kICC));
    gainICC = appBlocks(gainBlock, 0xe2, kICC, sizeof(kICC));
    const auto isoBlocks = appBlocks(gainBlock, 0xe2, kISO, sizeof(kISO));
    gainISO = isoBlocks.empty() ? legacyGainISO(*metadata) : isoBlocks.front();
    base = decodeJpegRGB(baseBlock->data, baseBlock->data_sz, kMaxTilePlanePixels);
    gain = decodeJpegRGB(gainBlock->data, gainBlock->data_sz, kMaxTilePlanePixels);
    uhdr_release_decoder(dec);
  } else {
    uhdr_mem_block_t src{bytes.data(), bytes.size(), bytes.size()};
    baseICC = appBlocks(&src, 0xe2, kICC, sizeof(kICC));
    const auto exifBlocks = appBlocks(&src, 0xe1, "Exif\0", 6);
    if (!exifBlocks.empty()) {
      const auto& block = exifBlocks.front();
      const uhdr_mem_block_t exif{const_cast<unsigned char*>(block.data()), block.size(), block.size()};
      orientation = exifOrientation(&exif);
    }
    base = decodeJpegRGB(bytes.data(), bytes.size(), kMaxTilePlanePixels);
  }
  Bytes().swap(bytes);

  if (baseICC.empty()) {
    Bytes icc(kICC, kICC + sizeof(kICC));
    icc.insert(icc.end(), {1, 1});
    icc.insert(icc.end(), kSRGBICC, kSRGBICC + kSRGBICC_len);
    baseICC.push_back(std::move(icc));
    base.grayscale = false;  // The fallback sRGB profile describes RGB pixels.
  }
  const int width = orientation >= 5 ? base.h : base.w;
  const int height = orientation >= 5 ? base.w : base.h;
  const int longest = std::max(width, height);
  const double scale = std::min(1.0, static_cast<double>(kTilePreviewLimit) / longest);
  const int previewWidth = std::max(1, static_cast<int>(std::round(width * scale)));
  const int previewHeight = std::max(1, static_cast<int>(std::round(height * scale)));
  const auto write = [&](const std::string& path, int lw, int lh, int l, int t, int tw, int th) {
    const auto baseTile = sampleTile(base, orientation, lw, lh, l, t, tw, th);
    if (hdr) {
      const auto gainTile = sampleTile(gain, orientation, lw, lh, l, t, tw, th);
      writeHDRTile(path, baseTile, gainTile, baseICC, gainICC, gainISO);
    } else {
      writeSDRTile(path, baseTile, baseICC);
    }
  };
  write(directory + "/preview.jpg", previewWidth, previewHeight, 0, 0, previewWidth, previewHeight);
  int maxLevel = 0;
  for (int n = longest - 1; n > 0; n >>= 1) maxLevel++;
  for (int z = 0; z <= maxLevel; z++) {
    const int divisor = 1 << (maxLevel - z);
    const int lw = (width + divisor - 1) / divisor;
    const int lh = (height + divisor - 1) / divisor;
    if (std::max(lw, lh) <= kTilePreviewLimit) continue;
    const std::string levelPath = directory + "/" + std::to_string(z);
    std::error_code ec;
    std::filesystem::create_directory(levelPath, ec);
    if (ec) die("create tile level directory: " + ec.message());
    for (int y = 0; y * kTileSize < lh; y++) {
      for (int x = 0; x * kTileSize < lw; x++) {
        const int l = std::max(0, x * kTileSize - kTileOverlap);
        const int t = std::max(0, y * kTileSize - kTileOverlap);
        const int r = std::min(lw, (x + 1) * kTileSize + kTileOverlap);
        const int b = std::min(lh, (y + 1) * kTileSize + kTileOverlap);
        write(levelPath + "/" + std::to_string(x) + "_" + std::to_string(y) + ".jpg", lw, lh, l, t, r - l, b - t);
      }
    }
  }
  std::cout << "{\"width\":" << width << ",\"height\":" << height
            << ",\"previewWidth\":" << previewWidth << ",\"previewHeight\":" << previewHeight << "}\n";
}
