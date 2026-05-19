#include "ultrahdr_api.h"
#include <algorithm>
#include <cctype>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

static void die(const std::string& msg){ std::cerr << msg << "\n"; std::exit(1); }
static void check(uhdr_error_info_t e, const char* what){ if(e.error_code != UHDR_CODEC_OK){ std::string m=what; if(e.has_detail){ m += ": "; m += e.detail; } die(m);} }
static std::vector<unsigned char> readFile(const std::string& p){ std::ifstream f(p, std::ios::binary); if(!f) die("open input failed: "+p); return std::vector<unsigned char>((std::istreambuf_iterator<char>(f)), {}); }
static void writeFile(const std::string& p, const void* data, size_t n){ std::ofstream f(p, std::ios::binary); if(!f) die("open output failed: "+p); f.write((const char*)data, n); if(!f) die("write output failed: "+p); }
struct Size { int w=0,h=0; char mode=0; };
struct Geometry { int resizeW=0, resizeH=0, cropLeft=0, cropTop=0, cropRight=0, cropBottom=0; bool crop=false; };
static int scaled(int n,double scale){ return std::max(1,(int)(n*scale+0.5)); }
static Size parseSize(const std::string& s){ Size z; size_t x=s.find('x'); if(x==std::string::npos) die("bad size"); z.w=std::stoi(s.substr(0,x)); size_t i=x+1; size_t j=i; while(j<s.size() && isdigit((unsigned char)s[j])) j++; z.h=std::stoi(s.substr(i,j-i)); if(j<s.size()) z.mode=s[j]; if(j+1<s.size()) die("bad size suffix"); if(z.w==0 && z.h==0) die("bad zero size"); return z; }
static void targetContain(int sw,int sh,const Size& s,int& tw,int& th){ if(s.w==0){ th=s.h; tw=(int)((long long)sw*th/sh); } else if(s.h==0){ tw=s.w; th=(int)((long long)sh*tw/sw); } else { tw=s.w; th=s.h; } if(tw<1)tw=1; if(th<1)th=1; }
static void targetFit(int sw,int sh,const Size& s,int& tw,int& th){ if(s.w==0 || s.h==0 || (s.mode!='f')) return targetContain(sw,sh,s,tw,th); double scale=std::min((double)s.w/sw,(double)s.h/sh); tw=scaled(sw,scale); th=scaled(sh,scale); }
static Geometry targetGeometry(int sw,int sh,const Size& s){ Geometry g; if(s.w==0 || s.h==0 || s.mode=='f'){ targetFit(sw,sh,s,g.resizeW,g.resizeH); return g; }
  double scale=std::max((double)s.w/sw,(double)s.h/sh); g.resizeW=scaled(sw,scale); g.resizeH=scaled(sh,scale);
  int extraX=std::max(0,g.resizeW-s.w), extraY=std::max(0,g.resizeH-s.h); g.cropLeft=extraX/2; g.cropTop=extraY/2; if(s.mode=='t') g.cropTop=0; if(s.mode=='b') g.cropTop=extraY; g.cropRight=g.cropLeft+s.w; g.cropBottom=g.cropTop+s.h; g.crop=true; return g; }
static uhdr_compressed_image_t comp(std::vector<unsigned char>& b){ uhdr_compressed_image_t img{}; img.data=b.data(); img.data_sz=b.size(); img.capacity=b.size(); img.cg=UHDR_CG_UNSPECIFIED; img.ct=UHDR_CT_UNSPECIFIED; img.range=UHDR_CR_UNSPECIFIED; return img; }
static void probe(const std::string& in){ auto bytes=readFile(in); if(!is_uhdr_image(bytes.data(), (int)bytes.size())) die("not a valid Ultra HDR image"); auto* dec=uhdr_create_decoder(); if(!dec) die("decoder allocation failed"); auto img=comp(bytes); check(uhdr_dec_set_image(dec,&img),"set image"); check(uhdr_dec_set_out_img_format(dec,UHDR_IMG_FMT_32bppRGBA8888),"set decode format"); check(uhdr_dec_set_out_color_transfer(dec,UHDR_CT_SRGB),"set decode transfer"); check(uhdr_dec_probe(dec),"probe"); int w=uhdr_dec_get_image_width(dec), h=uhdr_dec_get_image_height(dec), gw=uhdr_dec_get_gainmap_width(dec), gh=uhdr_dec_get_gainmap_height(dec); auto* md=uhdr_dec_get_gainmap_metadata(dec); check(uhdr_decode(dec),"decode"); uhdr_raw_image_t* raw=uhdr_get_decoded_image(dec); if(!raw) die("decoded image missing"); std::cout << "{\"width\":" << w << ",\"height\":" << h
          << ",\"gainmap_width\":" << gw << ",\"gainmap_height\":" << gh
          << ",\"metadata\":" << (md?"true":"false") << ",\"decoded_width\":" << raw->w << ",\"decoded_height\":" << raw->h << "}\n"; uhdr_release_decoder(dec); }
static void resize(const std::string& in,const std::string& out,const std::string& sizeStr){ auto bytes=readFile(in); if(!is_uhdr_image(bytes.data(), (int)bytes.size())) die("not a valid Ultra HDR image"); auto img=comp(bytes);
  auto* p=uhdr_create_decoder(); if(!p) die("probe decoder allocation failed"); check(uhdr_dec_set_image(p,&img),"set image"); check(uhdr_dec_probe(p),"probe"); int sw=uhdr_dec_get_image_width(p), sh=uhdr_dec_get_image_height(p); uhdr_release_decoder(p);
  Geometry geom=targetGeometry(sw,sh,parseSize(sizeStr));
  auto* dec=uhdr_create_decoder(); if(!dec) die("decoder allocation failed"); check(uhdr_dec_set_image(dec,&img),"set image"); check(uhdr_dec_set_out_img_format(dec,UHDR_IMG_FMT_32bppRGBA1010102),"set format"); check(uhdr_dec_set_out_color_transfer(dec,UHDR_CT_HLG),"set transfer"); check(uhdr_add_effect_resize(dec,geom.resizeW,geom.resizeH),"resize effect"); if(geom.crop){ check(uhdr_add_effect_crop(dec,geom.cropLeft,geom.cropRight,geom.cropTop,geom.cropBottom),"crop effect"); } check(uhdr_decode(dec),"decode"); uhdr_raw_image_t* raw=uhdr_get_decoded_image(dec); if(!raw) die("decoded image missing");
  uhdr_raw_image_t encRaw=*raw; encRaw.range=UHDR_CR_FULL_RANGE;
  auto* enc=uhdr_create_encoder(); if(!enc) die("encoder allocation failed"); check(uhdr_enc_set_raw_image(enc,&encRaw,UHDR_HDR_IMG),"set hdr raw"); check(uhdr_enc_set_quality(enc,90,UHDR_BASE_IMG),"set base quality"); check(uhdr_enc_set_quality(enc,90,UHDR_GAIN_MAP_IMG),"set gainmap quality"); check(uhdr_enc_set_output_format(enc,UHDR_CODEC_JPG),"set output format"); check(uhdr_encode(enc),"encode"); uhdr_compressed_image_t* outimg=uhdr_get_encoded_stream(enc); if(!outimg) die("encoded stream missing"); writeFile(out,outimg->data,outimg->data_sz); uhdr_release_encoder(enc); uhdr_release_decoder(dec); }
int main(int argc,char** argv){ if(argc<3) die("usage: hdrthumb-helper probe <in> | resize <in> <out> <size>"); std::string cmd=argv[1]; if(cmd=="probe" && argc==3){ probe(argv[2]); return 0; } if(cmd=="resize" && argc==5){ resize(argv[2],argv[3],argv[4]); return 0; } die("bad arguments"); }
