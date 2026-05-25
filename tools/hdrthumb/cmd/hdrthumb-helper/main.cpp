#include "ultrahdr_api.h"

#include <cstdio>
#include <jpeglib.h>
#include <setjmp.h>

#include <algorithm>
#include <cctype>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

static void die(const std::string& msg) {
  std::cerr << msg << "\n";
  std::exit(1);
}

static void check(uhdr_error_info_t e, const char* what) {
  if (e.error_code != UHDR_CODEC_OK) {
    std::string m = what;
    if (e.has_detail) {
      m += ": ";
      m += e.detail;
    }
    die(m);
  }
}

static std::vector<unsigned char> readFile(const std::string& p) {
  std::ifstream f(p, std::ios::binary);
  if (!f) die("open input failed: " + p);
  return std::vector<unsigned char>((std::istreambuf_iterator<char>(f)), {});
}

static void writeFile(const std::string& p, const void* data, size_t n) {
  std::ofstream f(p, std::ios::binary);
  if (!f) die("open output failed: " + p);
  f.write(static_cast<const char*>(data), n);
  if (!f) die("write output failed: " + p);
}

struct Size {
  int w = 0;
  int h = 0;
  char mode = 0;
};

struct Geometry {
  int srcLeft = 0;
  int srcTop = 0;
  int srcW = 0;
  int srcH = 0;
  int resizeW = 0;
  int resizeH = 0;
};

struct RGBImage {
  int w = 0;
  int h = 0;
  std::vector<unsigned char> px;
};

struct JpegError {
  jpeg_error_mgr pub;
  jmp_buf jump;
  char msg[JMSG_LENGTH_MAX];
};

static void jpegErrorExit(j_common_ptr cinfo) {
  auto* e = reinterpret_cast<JpegError*>(cinfo->err);
  (*cinfo->err->format_message)(cinfo, e->msg);
  longjmp(e->jump, 1);
}

static int rounded(double v) { return std::max(1, static_cast<int>(v + 0.5)); }

static Size parseSize(const std::string& s) {
  Size z;
  size_t x = s.find('x');
  if (x == std::string::npos) die("bad size");

  z.w = std::stoi(s.substr(0, x));
  size_t i = x + 1;
  size_t j = i;
  while (j < s.size() && isdigit(static_cast<unsigned char>(s[j]))) j++;
  z.h = std::stoi(s.substr(i, j - i));
  if (j < s.size()) z.mode = s[j];
  if (j + 1 < s.size()) die("bad size suffix");
  if (z.w == 0 && z.h == 0) die("bad zero size");
  return z;
}

static void targetContain(int sw, int sh, const Size& s, int& tw, int& th) {
  if (s.w == 0) {
    th = s.h;
    tw = rounded(static_cast<double>(th) * sw / sh);
  } else if (s.h == 0) {
    tw = s.w;
    th = rounded(static_cast<double>(tw) * sh / sw);
  } else {
    tw = s.w;
    th = s.h;
  }
}

static void targetFit(int sw, int sh, const Size& s, int& tw, int& th) {
  if (s.w == 0 || s.h == 0 || s.mode != 'f') return targetContain(sw, sh, s, tw, th);
  if (sw <= s.w && sh <= s.h) {
    tw = sw;
    th = sh;
    return;
  }
  double srcAspect = static_cast<double>(sw) / sh;
  double maxAspect = static_cast<double>(s.w) / s.h;
  if (srcAspect > maxAspect) {
    tw = s.w;
    th = std::max(1, static_cast<int>(static_cast<double>(tw) / srcAspect));
  } else {
    th = s.h;
    tw = std::max(1, static_cast<int>(static_cast<double>(th) * srcAspect));
  }
}

static int anchorOffset(int srcN, int cropN, char mode) {
  if (mode == 't') return 0;
  if (mode == 'b') return srcN - cropN;
  return (srcN - cropN) / 2;
}

static Geometry targetGeometry(int sw, int sh, const Size& s) {
  Geometry g;
  g.srcW = sw;
  g.srcH = sh;

  if (s.w == 0 || s.h == 0 || s.mode == 'f') {
    targetFit(sw, sh, s, g.resizeW, g.resizeH);
    return g;
  }

  g.resizeW = s.w;
  g.resizeH = s.h;

  // Match imaging.Fill for normal images: crop source to destination aspect, then resize.
  if (sw >= 100 && sh >= 100) {
    double srcAspect = static_cast<double>(sw) / sh;
    double dstAspect = static_cast<double>(s.w) / s.h;
    if (srcAspect < dstAspect) {
      g.srcH = rounded(static_cast<double>(sw) * s.h / s.w);
      g.srcTop = anchorOffset(sh, g.srcH, s.mode);
    } else {
      g.srcW = rounded(static_cast<double>(sh) * s.w / s.h);
      g.srcLeft = (sw - g.srcW) / 2;
    }
    return g;
  }

  // Small-image fallback mirrors imaging's resize-then-crop dimensions.
  double scale = std::max(static_cast<double>(s.w) / sw, static_cast<double>(s.h) / sh);
  int scaledW = rounded(sw * scale);
  int scaledH = rounded(sh * scale);
  g.srcLeft = 0;
  g.srcTop = 0;
  g.srcW = sw;
  g.srcH = sh;
  g.resizeW = std::min(s.w, scaledW);
  g.resizeH = std::min(s.h, scaledH);
  return g;
}

static int outW(const Geometry& g) { return g.resizeW; }
static int outH(const Geometry& g) { return g.resizeH; }

static int clampInt(int v, int lo, int hi) { return std::max(lo, std::min(hi, v)); }

static double sourceCoord(int out, int srcOffset, int srcN, int dstN) {
  return srcOffset + (static_cast<double>(out) + 0.5) * srcN / dstN - 0.5;
}

static unsigned char lerp8(unsigned char c00, unsigned char c10, unsigned char c01, unsigned char c11,
                           double fx, double fy) {
  double top = c00 + (c10 - c00) * fx;
  double bottom = c01 + (c11 - c01) * fx;
  int v = static_cast<int>(top + (bottom - top) * fy + 0.5);
  return static_cast<unsigned char>(clampInt(v, 0, 255));
}

static uint32_t lerp10(uint32_t c00, uint32_t c10, uint32_t c01, uint32_t c11, double fx, double fy) {
  double top = c00 + (static_cast<double>(c10) - c00) * fx;
  double bottom = c01 + (static_cast<double>(c11) - c01) * fx;
  int v = static_cast<int>(top + (bottom - top) * fy + 0.5);
  return static_cast<uint32_t>(clampInt(v, 0, 1023));
}

static uhdr_compressed_image_t comp(std::vector<unsigned char>& b) {
  uhdr_compressed_image_t img{};
  img.data = b.data();
  img.data_sz = b.size();
  img.capacity = b.size();
  img.cg = UHDR_CG_UNSPECIFIED;
  img.ct = UHDR_CT_UNSPECIFIED;
  img.range = UHDR_CR_UNSPECIFIED;
  return img;
}

static RGBImage decodeJpegRGB(const void* data, size_t size) {
  jpeg_decompress_struct cinfo{};
  JpegError err{};
  cinfo.err = jpeg_std_error(&err.pub);
  err.pub.error_exit = jpegErrorExit;
  if (setjmp(err.jump)) {
    jpeg_destroy_decompress(&cinfo);
    die(std::string("jpeg decode failed: ") + err.msg);
  }

  jpeg_create_decompress(&cinfo);
  jpeg_mem_src(&cinfo, static_cast<const unsigned char*>(data), size);
  jpeg_read_header(&cinfo, TRUE);
  cinfo.out_color_space = JCS_RGB;
  jpeg_start_decompress(&cinfo);

  RGBImage img;
  img.w = static_cast<int>(cinfo.output_width);
  img.h = static_cast<int>(cinfo.output_height);
  img.px.resize(static_cast<size_t>(img.w) * img.h * 3);
  while (cinfo.output_scanline < cinfo.output_height) {
    unsigned char* row = &img.px[static_cast<size_t>(cinfo.output_scanline) * img.w * 3];
    jpeg_read_scanlines(&cinfo, &row, 1);
  }

  jpeg_finish_decompress(&cinfo);
  jpeg_destroy_decompress(&cinfo);
  return img;
}

static RGBImage resizeRGB(const RGBImage& src, const Geometry& g) {
  RGBImage dst;
  dst.w = outW(g);
  dst.h = outH(g);
  dst.px.resize(static_cast<size_t>(dst.w) * dst.h * 3);

  for (int y = 0; y < dst.h; y++) {
    double srcY = sourceCoord(y, g.srcTop, g.srcH, g.resizeH);
    int y0 = clampInt(static_cast<int>(srcY), 0, src.h - 1);
    int y1 = clampInt(y0 + 1, 0, src.h - 1);
    double fy = std::max(0.0, std::min(1.0, srcY - y0));
    for (int x = 0; x < dst.w; x++) {
      double srcX = sourceCoord(x, g.srcLeft, g.srcW, g.resizeW);
      int x0 = clampInt(static_cast<int>(srcX), 0, src.w - 1);
      int x1 = clampInt(x0 + 1, 0, src.w - 1);
      double fx = std::max(0.0, std::min(1.0, srcX - x0));
      unsigned char* dp = &dst.px[(static_cast<size_t>(y) * dst.w + x) * 3];
      const unsigned char* p00 = &src.px[(static_cast<size_t>(y0) * src.w + x0) * 3];
      const unsigned char* p10 = &src.px[(static_cast<size_t>(y0) * src.w + x1) * 3];
      const unsigned char* p01 = &src.px[(static_cast<size_t>(y1) * src.w + x0) * 3];
      const unsigned char* p11 = &src.px[(static_cast<size_t>(y1) * src.w + x1) * 3];
      for (int c = 0; c < 3; c++) dp[c] = lerp8(p00[c], p10[c], p01[c], p11[c], fx, fy);
    }
  }
  return dst;
}

static std::vector<uint32_t> resizePacked32(const uhdr_raw_image_t* src, const Geometry& g) {
  int dw = outW(g);
  int dh = outH(g);
  std::vector<uint32_t> dst(static_cast<size_t>(dw) * dh);
  const auto* in = static_cast<const uint32_t*>(src->planes[UHDR_PLANE_PACKED]);
  unsigned int stride = src->stride[UHDR_PLANE_PACKED] ? src->stride[UHDR_PLANE_PACKED] : src->w;

  for (int y = 0; y < dh; y++) {
    double srcY = sourceCoord(y, g.srcTop, g.srcH, g.resizeH);
    int y0 = clampInt(static_cast<int>(srcY), 0, static_cast<int>(src->h) - 1);
    int y1 = clampInt(y0 + 1, 0, static_cast<int>(src->h) - 1);
    double fy = std::max(0.0, std::min(1.0, srcY - y0));
    for (int x = 0; x < dw; x++) {
      double srcX = sourceCoord(x, g.srcLeft, g.srcW, g.resizeW);
      int x0 = clampInt(static_cast<int>(srcX), 0, static_cast<int>(src->w) - 1);
      int x1 = clampInt(x0 + 1, 0, static_cast<int>(src->w) - 1);
      double fx = std::max(0.0, std::min(1.0, srcX - x0));
      uint32_t p00 = in[static_cast<size_t>(y0) * stride + x0];
      uint32_t p10 = in[static_cast<size_t>(y0) * stride + x1];
      uint32_t p01 = in[static_cast<size_t>(y1) * stride + x0];
      uint32_t p11 = in[static_cast<size_t>(y1) * stride + x1];
      uint32_t r = lerp10(p00 & 0x3ff, p10 & 0x3ff, p01 & 0x3ff, p11 & 0x3ff, fx, fy);
      uint32_t gc = lerp10((p00 >> 10) & 0x3ff, (p10 >> 10) & 0x3ff, (p01 >> 10) & 0x3ff,
                           (p11 >> 10) & 0x3ff, fx, fy);
      uint32_t b = lerp10((p00 >> 20) & 0x3ff, (p10 >> 20) & 0x3ff, (p01 >> 20) & 0x3ff,
                          (p11 >> 20) & 0x3ff, fx, fy);
      dst[static_cast<size_t>(y) * dw + x] = r | (gc << 10) | (b << 20) | (0x3u << 30);
    }
  }
  return dst;
}

static std::vector<uint32_t> packRGBAToLittleEndian(const RGBImage& img) {
  std::vector<uint32_t> packed(static_cast<size_t>(img.w) * img.h);
  for (size_t i = 0; i < packed.size(); i++) {
    const unsigned char* p = &img.px[i * 3];
    packed[i] = static_cast<uint32_t>(p[0]) | (static_cast<uint32_t>(p[1]) << 8) |
                (static_cast<uint32_t>(p[2]) << 16) | (0xffu << 24);
  }
  return packed;
}

static void probe(const std::string& in) {
  auto bytes = readFile(in);
  if (!is_uhdr_image(bytes.data(), static_cast<int>(bytes.size()))) die("not a valid Ultra HDR image");

  auto* dec = uhdr_create_decoder();
  if (!dec) die("decoder allocation failed");
  auto img = comp(bytes);
  check(uhdr_dec_set_image(dec, &img), "set image");
  check(uhdr_dec_set_out_img_format(dec, UHDR_IMG_FMT_32bppRGBA8888), "set decode format");
  check(uhdr_dec_set_out_color_transfer(dec, UHDR_CT_SRGB), "set decode transfer");
  check(uhdr_dec_probe(dec), "probe");
  int w = uhdr_dec_get_image_width(dec);
  int h = uhdr_dec_get_image_height(dec);
  int gw = uhdr_dec_get_gainmap_width(dec);
  int gh = uhdr_dec_get_gainmap_height(dec);
  auto* md = uhdr_dec_get_gainmap_metadata(dec);
  check(uhdr_decode(dec), "decode");
  uhdr_raw_image_t* raw = uhdr_get_decoded_image(dec);
  if (!raw) die("decoded image missing");
  std::cout << "{\"width\":" << w << ",\"height\":" << h << ",\"gainmap_width\":" << gw
            << ",\"gainmap_height\":" << gh << ",\"metadata\":" << (md ? "true" : "false")
            << ",\"decoded_width\":" << raw->w << ",\"decoded_height\":" << raw->h << "}\n";
  uhdr_release_decoder(dec);
}

static void resize(const std::string& in, const std::string& out, const std::string& sizeStr) {
  auto bytes = readFile(in);
  if (!is_uhdr_image(bytes.data(), static_cast<int>(bytes.size()))) die("not a valid Ultra HDR image");
  auto img = comp(bytes);

  auto* p = uhdr_create_decoder();
  if (!p) die("probe decoder allocation failed");
  check(uhdr_dec_set_image(p, &img), "set image");
  check(uhdr_dec_probe(p), "probe");
  int sw = uhdr_dec_get_image_width(p);
  int sh = uhdr_dec_get_image_height(p);
  uhdr_mem_block_t* base = uhdr_dec_get_base_image(p);
  if (!base) die("base image missing");
  Geometry geom = targetGeometry(sw, sh, parseSize(sizeStr));
  RGBImage baseRGB = resizeRGB(decodeJpegRGB(base->data, base->data_sz), geom);
  uhdr_release_decoder(p);

  auto* dec = uhdr_create_decoder();
  if (!dec) die("decoder allocation failed");
  check(uhdr_dec_set_image(dec, &img), "set image");
  check(uhdr_dec_set_out_img_format(dec, UHDR_IMG_FMT_32bppRGBA1010102), "set format");
  check(uhdr_dec_set_out_color_transfer(dec, UHDR_CT_HLG), "set transfer");
  check(uhdr_decode(dec), "decode");
  uhdr_raw_image_t* raw = uhdr_get_decoded_image(dec);
  if (!raw) die("decoded image missing");

  // Root-cause fix: set both encoder intents. Encoding from HDR raw only lets libultrahdr synthesize
  // a new SDR/base layer from the rendered HLG image; that base layer can drift heavily from the
  // original composition. Keep the thumbnail's SDR/base layer as a resized original base JPEG while
  // still deriving a resized HDR rendition/gain map from the decoded Ultra HDR image.
  std::vector<uint32_t> hdrPixels = resizePacked32(raw, geom);
  uhdr_raw_image_t encHdr = *raw;
  encHdr.w = outW(geom);
  encHdr.h = outH(geom);
  encHdr.planes[UHDR_PLANE_PACKED] = hdrPixels.data();
  encHdr.stride[UHDR_PLANE_PACKED] = encHdr.w;
  encHdr.range = UHDR_CR_FULL_RANGE;

  std::vector<uint32_t> sdrPixels = packRGBAToLittleEndian(baseRGB);
  uhdr_raw_image_t encSdr{};
  encSdr.fmt = UHDR_IMG_FMT_32bppRGBA8888;
  encSdr.cg = UHDR_CG_BT_709;
  encSdr.ct = UHDR_CT_SRGB;
  encSdr.range = UHDR_CR_FULL_RANGE;
  encSdr.w = baseRGB.w;
  encSdr.h = baseRGB.h;
  encSdr.planes[UHDR_PLANE_PACKED] = sdrPixels.data();
  encSdr.stride[UHDR_PLANE_PACKED] = baseRGB.w;

  auto* enc = uhdr_create_encoder();
  if (!enc) die("encoder allocation failed");
  check(uhdr_enc_set_raw_image(enc, &encHdr, UHDR_HDR_IMG), "set hdr raw");
  check(uhdr_enc_set_raw_image(enc, &encSdr, UHDR_SDR_IMG), "set sdr raw");
  check(uhdr_enc_set_quality(enc, 90, UHDR_BASE_IMG), "set base quality");
  check(uhdr_enc_set_quality(enc, 90, UHDR_GAIN_MAP_IMG), "set gainmap quality");
  check(uhdr_enc_set_output_format(enc, UHDR_CODEC_JPG), "set output format");
  check(uhdr_encode(enc), "encode");
  uhdr_compressed_image_t* outimg = uhdr_get_encoded_stream(enc);
  if (!outimg) die("encoded stream missing");
  writeFile(out, outimg->data, outimg->data_sz);
  uhdr_release_encoder(enc);
  uhdr_release_decoder(dec);
}

int main(int argc, char** argv) {
  if (argc < 3) die("usage: hdrthumb-helper probe <in> | resize <in> <out> <size>");
  std::string cmd = argv[1];
  if (cmd == "probe" && argc == 3) {
    probe(argv[2]);
    return 0;
  }
  if (cmd == "resize" && argc == 5) {
    resize(argv[2], argv[3], argv[4]);
    return 0;
  }
  die("bad arguments");
}
