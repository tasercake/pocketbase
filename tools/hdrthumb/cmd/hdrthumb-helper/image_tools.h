#pragma once

#include "ultrahdr_api.h"

#include <cstddef>
#include <string>
#include <vector>

struct RGBImage {
  bool grayscale = false;
  int w = 0;
  int h = 0;
  std::vector<unsigned char> px;
};

[[noreturn]] void die(const std::string& message);
void check(uhdr_error_info_t error, const char* operation);
std::vector<unsigned char> readFile(const std::string& path);
void writeFile(const std::string& path, const void* data, size_t size);
double sourceCoord(int out, int srcOffset, int srcN, int dstN);
uhdr_compressed_image_t comp(std::vector<unsigned char>& data);
RGBImage decodeJpegRGB(const void* data, size_t size, size_t maxPixels = 0);
std::vector<unsigned char> encodeJpeg(const RGBImage& image, bool progressive);
void generateTiles(const std::string& input, const std::string& directory, bool hdr);
