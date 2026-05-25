//go:build hdr_thumbs

package hdrthumb

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
)

type helperProbeResult struct {
	Width         int  `json:"width"`
	Height        int  `json:"height"`
	GainmapWidth  int  `json:"gainmap_width"`
	GainmapHeight int  `json:"gainmap_height"`
	Metadata      bool `json:"metadata"`
	DecodedWidth  int  `json:"decoded_width"`
	DecodedHeight int  `json:"decoded_height"`
}

func TestUltraHDRBackendCreatesLibUltraHDRThumbnail(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Create(input, Options{Size: "320x0", OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ContentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg result, got %q", result.ContentType)
	}
	if len(result.Bytes) == 0 {
		t.Fatal("expected thumbnail bytes")
	}

	probe := probeWithLibUltraHDR(t, result.Bytes)
	if probe.Width != 320 || probe.Height == 0 {
		t.Fatalf("unexpected libultrahdr dimensions: %+v", probe)
	}
	if probe.DecodedWidth != probe.Width || probe.DecodedHeight != probe.Height {
		t.Fatalf("libultrahdr decode/probe dimensions differ: %+v", probe)
	}
	if !probe.Metadata || probe.GainmapWidth != probe.Width || probe.GainmapHeight != probe.Height {
		t.Fatalf("unexpected gain-map metadata/dimensions: %+v", probe)
	}

	cfg, err := jpeg.DecodeConfig(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != probe.Width || cfg.Height != probe.Height {
		t.Fatalf("JPEG config and libultrahdr dimensions differ: jpeg=%dx%d probe=%+v", cfg.Width, cfg.Height, probe)
	}

	img, _, err := image.Decode(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		t.Fatalf("decode/config dimensions differ: decode=%v config=%dx%d", img.Bounds(), cfg.Width, cfg.Height)
	}
}

func TestUltraHDRBackendGeometryModes(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		size       string
		wantWidth  int
		wantHeight int
	}{
		{name: "width preserves aspect", size: "160x0", wantWidth: 160, wantHeight: 213},
		{name: "height preserves aspect", size: "0x80", wantWidth: 60, wantHeight: 80},
		{name: "center crop", size: "160x80", wantWidth: 160, wantHeight: 80},
		{name: "top crop", size: "160x80t", wantWidth: 160, wantHeight: 80},
		{name: "bottom crop", size: "160x80b", wantWidth: 160, wantHeight: 80},
		{name: "fit", size: "160x80f", wantWidth: 59, wantHeight: 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Create(input, Options{Size: tt.size, OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
			if err != nil {
				t.Fatal(err)
			}
			probe := probeWithLibUltraHDR(t, result.Bytes)
			if probe.Width != tt.wantWidth || probe.Height != tt.wantHeight {
				t.Fatalf("%s produced dimensions %dx%d, want %dx%d (probe: %+v)", tt.size, probe.Width, probe.Height, tt.wantWidth, tt.wantHeight, probe)
			}
			if probe.DecodedWidth != probe.Width || probe.DecodedHeight != probe.Height {
				t.Fatalf("libultrahdr decode/probe dimensions differ: %+v", probe)
			}
			if !probe.Metadata || probe.GainmapWidth != probe.Width || probe.GainmapHeight != probe.Height {
				t.Fatalf("unexpected gain-map metadata/dimensions: %+v", probe)
			}
		})
	}
}

func TestUltraHDRBackendPreservesBaseCompositionForAspectResize(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Create(input, Options{Size: "1200x0", OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	probe := probeWithLibUltraHDR(t, result.Bytes)
	if !probe.Metadata || probe.GainmapWidth != probe.Width || probe.GainmapHeight != probe.Height {
		t.Fatalf("thumbnail is not HDR-preserving: %+v", probe)
	}

	thumb, _, err := image.Decode(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	if thumb.Bounds().Dx() != 1200 || thumb.Bounds().Dy() != 1600 {
		t.Fatalf("unexpected thumbnail bounds: %v", thumb.Bounds())
	}
	if std := averageRGBStdDev(thumb); std < 5 {
		t.Fatalf("thumbnail appears solid-color/corrupt; average RGB stddev %.2f", std)
	}

	base, err := imaging.Decode(bytes.NewReader(input), imaging.AutoOrientation(true))
	if err != nil {
		t.Fatal(err)
	}
	reference := imaging.Resize(base, thumb.Bounds().Dx(), thumb.Bounds().Dy(), imaging.Linear)
	if diff := averagePixelDifference(thumb, reference); diff > 20 {
		t.Fatalf("thumbnail base composition drifted from ordinary aspect resize (average RGB delta %.2f)", diff)
	}
}

func TestUltraHDRBackendMatchesPocketBaseGeometrySemantics(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}

	base, err := imaging.Decode(bytes.NewReader(input), imaging.AutoOrientation(true))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		size      string
		reference *image.NRGBA
	}{
		{name: "center crop", size: "160x80", reference: imaging.Fill(base, 160, 80, imaging.Center, imaging.Linear)},
		{name: "top crop", size: "160x80t", reference: imaging.Fill(base, 160, 80, imaging.Top, imaging.Linear)},
		{name: "bottom crop", size: "160x80b", reference: imaging.Fill(base, 160, 80, imaging.Bottom, imaging.Linear)},
		{name: "fit", size: "160x80f", reference: imaging.Fit(base, 160, 80, imaging.Linear)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Create(input, Options{Size: tt.size, OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
			if err != nil {
				t.Fatal(err)
			}
			probe := probeWithLibUltraHDR(t, result.Bytes)
			if !probe.Metadata || probe.GainmapWidth != probe.Width || probe.GainmapHeight != probe.Height {
				t.Fatalf("thumbnail is not HDR-preserving: %+v", probe)
			}
			thumb, _, err := image.Decode(bytes.NewReader(result.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			if thumb.Bounds().Dx() != tt.reference.Bounds().Dx() || thumb.Bounds().Dy() != tt.reference.Bounds().Dy() {
				t.Fatalf("%s produced %v, want %v", tt.size, thumb.Bounds(), tt.reference.Bounds())
			}
			if diff := averagePixelDifference(thumb, tt.reference); diff > 20 {
				t.Fatalf("%s drifted from PocketBase geometry semantics (average RGB delta %.2f)", tt.size, diff)
			}
		})
	}
}

func averageRGBStdDev(img image.Image) float64 {
	b := img.Bounds()
	var sum [3]float64
	var sumSq [3]float64
	pixels := float64(b.Dx() * b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bv, _ := img.At(x, y).RGBA()
			vals := [3]float64{float64(r >> 8), float64(g >> 8), float64(bv >> 8)}
			for i, v := range vals {
				sum[i] += v
				sumSq[i] += v * v
			}
		}
	}
	var total float64
	for i := range sum {
		mean := sum[i] / pixels
		variance := sumSq[i]/pixels - mean*mean
		if variance > 0 {
			total += variance
		}
	}
	return math.Sqrt(total / 3)
}

func averagePixelDifference(a, b image.Image) float64 {
	ab := a.Bounds()
	bb := b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return 255
	}
	var total uint64
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			ar, ag, abv, _ := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bbv, _ := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			total += absDiff8(ar, br) + absDiff8(ag, bg) + absDiff8(abv, bbv)
		}
	}
	return float64(total) / float64(ab.Dx()*ab.Dy()*3)
}

func absDiff8(a, b uint32) uint64 {
	av := int(a >> 8)
	bv := int(b >> 8)
	if av > bv {
		return uint64(av - bv)
	}
	return uint64(bv - av)
}

func probeWithLibUltraHDR(t *testing.T, data []byte) helperProbeResult {
	t.Helper()
	helper := helperPath()
	if helper == "" {
		t.Fatal("hdrthumb-helper not found; run scripts/hdrthumb/build-libultrahdr.sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(helper, "probe", path).CombinedOutput()
	if err != nil {
		t.Fatalf("libultrahdr probe failed: %v: %s", err, string(output))
	}
	var probe helperProbeResult
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("failed to parse libultrahdr probe %q: %v", output, err)
	}
	return probe
}
