package hdrthumb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// PrimaryJPEGInfo describes only the first JPEG codestream in a JPEG_R file.
// Appended gain-map JPEG markers are deliberately excluded.
type PrimaryJPEGInfo struct {
	FirstSOF    byte
	SOSCount    int
	End         int
	HasICC      bool
	HasValidICC bool
	Width       int
	Height      int
	Is420       bool
}

// InspectPrimaryJPEG parses marker structure through the first EOI marker.
// It handles entropy-coded scan data, byte stuffing, and restart markers.
func InspectPrimaryJPEG(data []byte) (PrimaryJPEGInfo, error) {
	var info PrimaryJPEGInfo
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return info, errors.New("primary JPEG is missing SOI")
	}

	pos := 2
	inScan := false
	for pos < len(data) {
		if !inScan {
			if data[pos] != 0xff {
				return info, fmt.Errorf("unexpected primary JPEG byte at offset %d", pos)
			}
			for pos < len(data) && data[pos] == 0xff {
				pos++
			}
			if pos >= len(data) {
				break
			}
			marker := data[pos]
			pos++
			if marker == 0x00 {
				return info, fmt.Errorf("stuffed byte outside primary JPEG scan at offset %d", pos-1)
			}
			if marker == 0xd9 {
				info.End = pos
				return info, nil
			}
			if marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
				continue
			}
			if pos+2 > len(data) {
				break
			}
			length := int(data[pos])<<8 | int(data[pos+1])
			if length < 2 || pos+length > len(data) {
				return info, fmt.Errorf("invalid primary JPEG marker length at offset %d", pos-2)
			}
			payload := data[pos+2 : pos+length]
			if isSOFMarker(marker) && info.FirstSOF == 0 {
				info.FirstSOF = marker
				if len(payload) >= 15 && payload[5] == 3 {
					info.Height = int(payload[1])<<8 | int(payload[2])
					info.Width = int(payload[3])<<8 | int(payload[4])
					info.Is420 = payload[7] == 0x22 && payload[10] == 0x11 && payload[13] == 0x11
				}
			}
			if marker == 0xe2 && len(payload) >= 14 && string(payload[:12]) == "ICC_PROFILE\x00" {
				info.HasICC = true
				profile := payload[14:]
				if payload[12] == 1 && payload[13] == 1 && len(profile) >= 128 {
					declared := int(binary.BigEndian.Uint32(profile[:4]))
					info.HasValidICC = declared >= 128 && declared <= len(profile) && string(profile[36:40]) == "acsp"
				}
			}
			pos += length
			if marker == 0xda {
				info.SOSCount++
				inScan = true
			}
			continue
		}

		// Entropy-coded bytes continue until an unstuffed, non-restart marker.
		for pos < len(data) && data[pos] != 0xff {
			pos++
		}
		if pos >= len(data) {
			break
		}
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			break
		}
		marker := data[pos]
		if marker == 0x00 || (marker >= 0xd0 && marker <= 0xd7) {
			pos++
			continue
		}
		// Leave marker prefix available to normal marker parsing.
		pos--
		inScan = false
	}

	return info, errors.New("primary JPEG is missing EOI")
}

func isSOFMarker(marker byte) bool {
	if marker < 0xc0 || marker > 0xcf {
		return false
	}
	switch marker {
	case 0xc4, 0xc8, 0xcc:
		return false
	default:
		return true
	}
}

// ValidateProgressiveUltraHDR performs lightweight JPEG_R signature and primary
// codestream checks. Readiness/security decisions must additionally call Probe,
// which parses metadata offsets and performs a full pinned-libultrahdr decode.
func ValidateProgressiveUltraHDR(data []byte, contentType string) (PrimaryJPEGInfo, error) {
	detected, err := DetectBytes(data, contentType)
	if err != nil {
		return PrimaryJPEGInfo{}, err
	}
	if detected.Kind != KindUltraHDRJPEG {
		return PrimaryJPEGInfo{}, NewError(ErrHDRRequired, detected.Kind, "", "", "image is not Ultra HDR JPEG_R")
	}
	info, err := InspectPrimaryJPEG(data)
	if err != nil {
		return info, err
	}
	if info.FirstSOF != 0xc2 {
		return info, fmt.Errorf("primary JPEG is not progressive: first SOF marker is FF%02X", info.FirstSOF)
	}
	if info.SOSCount < 2 {
		return info, fmt.Errorf("primary progressive JPEG has %d scan; want multiple scans", info.SOSCount)
	}
	return info, nil
}
