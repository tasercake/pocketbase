package hdrthumb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectBytesCurrentPhotoFixtures(t *testing.T) {
	fixtures := []string{
		"current-photo-1.jpg",
		"current-photo-2.jpg",
		"current-photo-3.jpg",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "tests", "data", "hdr", name))
			if err != nil {
				t.Fatal(err)
			}

			got, err := DetectBytes(data, "image/jpeg")
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind == KindNone {
				t.Fatalf("DetectBytes() Kind = none, evidence: %#v", got.Evidence)
			}
			if got.Kind != KindUltraHDRJPEG {
				t.Fatalf("DetectBytes() kind = %q, want %q; evidence: %#v", got.Kind, KindUltraHDRJPEG, got.Evidence)
			}
			if got.ContentType != "image/jpeg" {
				t.Fatalf("DetectBytes() ContentType = %q, want image/jpeg", got.ContentType)
			}
		})
	}
}

func TestDetectBytesPlainJPEGIsNotHDR(t *testing.T) {
	plain := []byte{
		0xff, 0xd8,
		0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0xff, 0xd9,
	}

	got, err := DetectBytes(plain, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindNone {
		t.Fatalf("DetectBytes() kind = %q, want %q; evidence: %#v", got.Kind, KindNone, got.Evidence)
	}
	if got.ContentType != "image/jpeg" {
		t.Fatalf("DetectBytes() ContentType = %q, want image/jpeg", got.ContentType)
	}
}

func TestDetectBytesMIMEClaimsHDRContainers(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        Kind
	}{
		{name: "avif", contentType: "image/avif; charset=binary", want: KindHDRAVIF},
		{name: "heic", contentType: "image/heic", want: KindHDRHEIC},
		{name: "heif", contentType: "image/heif", want: KindHDRHEIC},
		{name: "jxl", contentType: "image/jxl", want: KindHDRJXL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectBytes(nil, tt.contentType)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.want {
				t.Fatalf("DetectBytes() kind = %q, want %q", got.Kind, tt.want)
			}
			if len(got.Evidence) == 0 {
				t.Fatalf("DetectBytes() evidence is empty")
			}
		})
	}
}

func TestDetectBytesHDRContainerSignatures(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want Kind
	}{
		{name: "avif ftyp", data: isoBMFF("avif"), want: KindHDRAVIF},
		{name: "heic ftyp", data: isoBMFF("heic"), want: KindHDRHEIC},
		{name: "jxl codestream", data: []byte{0xff, 0x0a}, want: KindHDRJXL},
		{name: "jxl container", data: []byte{0x00, 0x00, 0x00, 0x0c, 'J', 'X', 'L', ' ', 0x0d, 0x0a, 0x87, 0x0a}, want: KindHDRJXL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectBytes(tt.data, "")
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tt.want {
				t.Fatalf("DetectBytes() kind = %q, want %q; evidence: %#v", got.Kind, tt.want, got.Evidence)
			}
			if len(got.Evidence) == 0 {
				t.Fatalf("DetectBytes() evidence is empty")
			}
		})
	}
}

func TestDefaultBackendUnavailableWhenNotBuiltWithHDRThumbs(t *testing.T) {
	if Available() {
		t.Skip("HDR backend is available in this build")
	}
	input, err := os.ReadFile(filepath.Join("..", "..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Create(input, Options{Size: "100x100", OriginalName: "source.jpg", ThumbName: "thumb.jpg", OriginalContentType: "image/jpeg"})
	if !errors.Is(err, ErrHDRBackendUnavailable) {
		t.Fatalf("Create() error = %v, want ErrHDRBackendUnavailable", err)
	}
}

func isoBMFF(majorBrand string) []byte {
	return []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', majorBrand[0], majorBrand[1], majorBrand[2], majorBrand[3], 0x00, 0x00, 0x00, 0x00, majorBrand[0], majorBrand[1], majorBrand[2], majorBrand[3]}
}
