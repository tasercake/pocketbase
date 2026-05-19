package hdrthumb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// Kind identifies the HDR encoding model detected in an image.
type Kind string

const (
	KindNone             Kind = "none"
	KindUltraHDRJPEG     Kind = "ultrahdr_jpeg"
	KindAppleGainMapJPEG Kind = "apple_gain_map_jpeg"
	KindAdobeGainMapJPEG Kind = "adobe_gain_map_jpeg"
	KindHDRHEIC          Kind = "hdr_heic"
	KindHDRAVIF          Kind = "hdr_avif"
	KindHDRJXL           Kind = "hdr_jxl"
	KindUnknownHDR       Kind = "unknown_hdr"
)

// Detection contains the pure-Go detection result for an image byte stream.
type Detection struct {
	Kind        Kind
	ContentType string
	Evidence    []string
}

// Marker describes one JPEG marker segment discovered by ScanJPEGMarkers.
type Marker struct {
	Offset uint64
	Marker byte
	Name   string
	Length int
	Label  string
}

// DetectBytes detects known HDR/gain-map image structures without native dependencies.
//
// This detector intentionally avoids classifying a container or MIME type as HDR
// unless HDR/gain-map metadata is present; it does not decode pixels or generate thumbnails.
func DetectBytes(input []byte, contentType string) (Detection, error) {
	contentType = normalizeContentType(contentType)

	if len(input) >= 3 && bytes.Equal(input[:3], []byte{0xff, 0xd8, 0xff}) {
		return detectJPEG(input)
	}

	if detectedContentType, ok := detectISOBMFFContentType(input); ok {
		if contentType == "" {
			contentType = detectedContentType
		}
		return Detection{Kind: KindNone, ContentType: contentType}, nil
	}

	if isJXL(input) {
		if contentType == "" {
			contentType = "image/jxl"
		}
		return Detection{Kind: KindNone, ContentType: contentType}, nil
	}

	if contentType == "" {
		contentType = normalizeContentType(http.DetectContentType(input))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return Detection{Kind: KindNone, ContentType: contentType}, nil
}

// ScanJPEGMarkers returns a compact list of JPEG marker segments for diagnostics.
func ScanJPEGMarkers(data []byte) ([]Marker, error) {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, fmt.Errorf("not a JPEG stream")
	}

	markers := []Marker{{Offset: 0, Marker: 0xd8, Name: "SOI"}}
	pos := 2
	for pos < len(data) {
		for pos < len(data) && data[pos] != 0xff {
			pos++
		}
		if pos >= len(data) {
			break
		}
		off := pos
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			break
		}
		marker := data[pos]
		pos++

		m := Marker{Offset: uint64(off), Marker: marker, Name: markerName(marker)}
		if marker == 0xd9 || marker == 0xda || (marker >= 0xd0 && marker <= 0xd7) || marker == 0x01 {
			markers = append(markers, m)
			if marker == 0xda || marker == 0xd9 {
				break
			}
			continue
		}
		if pos+2 > len(data) {
			return markers, fmt.Errorf("truncated JPEG segment length at offset %d", off)
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return markers, fmt.Errorf("invalid JPEG segment length %d at offset %d", segLen, off)
		}
		payload := data[pos+2 : pos+segLen]
		m.Length = segLen
		m.Label = segmentLabel(payload)
		markers = append(markers, m)
		pos += segLen
	}

	return markers, nil
}

func detectJPEG(data []byte) (Detection, error) {
	res := Detection{Kind: KindNone, ContentType: "image/jpeg"}
	markers, err := ScanJPEGMarkers(data)
	if err != nil {
		res.Evidence = append(res.Evidence, err.Error())
	}

	var hasMPF, hasAdobeHDRGM, hasUltraHDR, hasAppleHDR bool
	for _, m := range markers {
		if !hasMPF && m.Name == "APP2" && strings.Contains(m.Label, "MPF") {
			hasMPF = true
			res.Evidence = append(res.Evidence, "JPEG APP2 MPF multi-picture segment present")
		}
		if !hasAdobeHDRGM && m.Name == "APP1" && strings.Contains(m.Label, "XMP") && bytes.Contains(data, []byte("hdrgm:Version")) {
			hasAdobeHDRGM = true
			res.Evidence = append(res.Evidence, "XMP hdrgm:Version gain-map metadata present")
		}
		if !hasUltraHDR && (strings.Contains(m.Label, "urn:iso:std:iso:ts:21496") || bytes.Contains(data, []byte("urn:iso:std:iso:ts:21496"))) {
			hasUltraHDR = true
			res.Evidence = append(res.Evidence, "ISO 21496 / JPEG_R Ultra HDR namespace present")
		}
		if !hasAppleHDR && (bytes.Contains(data, []byte("HDRGainMap")) || bytes.Contains(data, []byte("AuxiliaryImageType")) && bytes.Contains(bytes.ToLower(data), []byte("hdr"))) {
			hasAppleHDR = true
			res.Evidence = append(res.Evidence, "Apple-style auxiliary HDR gain-map metadata present")
		}
	}

	switch {
	case hasUltraHDR:
		res.Kind = KindUltraHDRJPEG
	case hasAdobeHDRGM && hasMPF:
		res.Kind = KindAdobeGainMapJPEG
	case hasAppleHDR:
		res.Kind = KindAppleGainMapJPEG
	case hasAdobeHDRGM:
		res.Kind = KindUnknownHDR
		res.Evidence = append(res.Evidence, "gain-map metadata present without recognized secondary image container")
	}

	return res, nil
}

func detectISOBMFFContentType(data []byte) (string, bool) {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return "", false
	}
	brands := []string{string(data[8:12])}
	for pos := 16; pos+4 <= len(data) && pos < 128; pos += 4 {
		brands = append(brands, string(data[pos:pos+4]))
	}
	for _, brand := range brands {
		switch brand {
		case "avif", "avis":
			return "image/avif", true
		case "heic", "heix", "hevc", "hevx":
			return "image/heic", true
		case "mif1", "msf1":
			return "image/heif", true
		}
	}
	return "", false
}

func isJXL(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xff && data[1] == 0x0a ||
		len(data) >= 12 && bytes.Equal(data[:12], []byte{0x00, 0x00, 0x00, 0x0c, 'J', 'X', 'L', ' ', 0x0d, 0x0a, 0x87, 0x0a})
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if contentType == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		return mediaType
	}
	return contentType
}

func markerName(marker byte) string {
	switch {
	case marker >= 0xe0 && marker <= 0xef:
		return fmt.Sprintf("APP%d", marker-0xe0)
	case marker >= 0xd0 && marker <= 0xd7:
		return fmt.Sprintf("RST%d", marker-0xd0)
	}
	switch marker {
	case 0xd8:
		return "SOI"
	case 0xd9:
		return "EOI"
	case 0xda:
		return "SOS"
	case 0xdb:
		return "DQT"
	case 0xc0:
		return "SOF0"
	case 0xc2:
		return "SOF2"
	case 0xc4:
		return "DHT"
	case 0xdd:
		return "DRI"
	case 0xfe:
		return "COM"
	default:
		return fmt.Sprintf("0x%02X", marker)
	}
}

func segmentLabel(payload []byte) string {
	limit := len(payload)
	if limit > 96 {
		limit = 96
	}
	prefix := payload[:limit]
	if i := bytes.IndexByte(prefix, 0); i >= 0 {
		prefix = prefix[:i]
	}
	label := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return -1
		}
		return r
	}, string(prefix))
	if label == "" {
		return "-"
	}
	return label
}
