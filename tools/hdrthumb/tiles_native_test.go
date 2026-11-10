//go:build hdr_thumbs

package hdrthumb

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeTileHelperWritesValidHDRPreviewAndOverlappingTiles(t *testing.T) {
	out := t.TempDir()
	input := "../../tests/data/hdr/current-photo-1.jpg"
	result, err := exec.Command(helperPath(), "tiles", input, out).CombinedOutput()
	if err != nil {
		t.Fatalf("generate tile pyramid: %v: %s", err, result)
	}
	for _, want := range []struct {
		path          string
		width, height int
	}{
		{"preview.jpg", 1500, 2000},
		{"12/0_0.jpg", 520, 520},
		{"12/1_0.jpg", 528, 520},
		{"12/5_7.jpg", 345, 287},
	} {
		data, err := os.ReadFile(filepath.Join(out, want.path))
		if err != nil {
			t.Fatal(err)
		}
		markers, err := ScanJPEGMarkers(data)
		if err != nil {
			t.Fatal(err)
		}
		var foundMPF bool
		for _, marker := range markers {
			if strings.Contains(marker.Label, "MPF") {
				foundMPF = true
				break
			}
			if marker.Marker == 0xdb {
				break
			}
		}
		if !foundMPF {
			t.Fatalf("%s: MPF must precede the first quantization table", want.path)
		}
		probe, err := ProbeContext(context.Background(), data)
		if err != nil {
			t.Fatalf("%s: %v", want.path, err)
		}
		if probe.Width != want.width || probe.Height != want.height || !probe.Metadata {
			t.Fatalf("%s: dimensions or gain-map metadata = %+v", want.path, probe)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "11")); !os.IsNotExist(err) {
		t.Fatalf("generated tiles for a level already covered by the preview: %v", err)
	}
}

func TestNativeTileHelperKeepsSDRJPEGSDR(t *testing.T) {
	src, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	primary, err := InspectPrimaryJPEG(src)
	if err != nil {
		t.Fatal(err)
	}
	// Use a genuine SDR rendition with no appended gain map.
	cfg, err := jpeg.Decode(bytes.NewReader(src[:primary.End]))
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "sdr.jpg")
	f, err := os.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(jpeg.Encode(f, cfg, &jpeg.Options{Quality: 90}), f.Close()); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if result, err := exec.Command(helperPath(), "tiles-sdr", in, out).CombinedOutput(); err != nil {
		t.Fatalf("generate SDR tile pyramid: %v: %s", err, result)
	}
	for _, name := range []string{"preview.jpg", "12/0_0.jpg"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		detection, err := DetectBytes(data, "image/jpeg")
		if err != nil || detection.Kind != KindNone {
			t.Fatalf("%s: expected SDR JPEG, got %+v (%v)", name, detection, err)
		}
	}
}

func TestNativeTileHelperAppliesEXIFOrientation(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			pixel := color.NRGBA{R: 255, A: 255}
			if x >= 16 {
				pixel = color.NRGBA{B: 255, A: 255}
			}
			src.SetNRGBA(x, y, pixel)
		}
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	// A big-endian TIFF IFD with Orientation=6 (90 degrees clockwise).
	exif := []byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, 6, 0, 0, 0, 0, 0, 0}
	in := filepath.Join(t.TempDir(), "rotated.jpg")
	input := append(append([]byte{0xff, 0xd8}, exif...), jpegData.Bytes()[2:]...)
	if err := os.WriteFile(in, input, 0600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if result, err := exec.Command(helperPath(), "tiles-sdr", in, out).CombinedOutput(); err != nil {
		t.Fatalf("generate rotated preview: %v: %s", err, result)
	}
	data, err := os.ReadFile(filepath.Join(out, "preview.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Bounds() != image.Rect(0, 0, 16, 32) {
		t.Fatalf("unexpected oriented bounds: %v", preview.Bounds())
	}
	topR, _, topB, _ := preview.At(8, 8).RGBA()
	bottomR, _, bottomB, _ := preview.At(8, 24).RGBA()
	if topR <= topB || bottomB <= bottomR {
		t.Fatal("preview did not rotate clockwise")
	}
}
