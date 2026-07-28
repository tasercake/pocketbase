//go:build hdr_thumbs

package hdrthumb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	HDRMin        int  `json:"hdr_min"`
	HDRMax        int  `json:"hdr_max"`
	HDRHighlights int  `json:"hdr_highlights"`
	HDRClipped    int  `json:"hdr_clipped"`
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
	primary, err := InspectPrimaryJPEG(result.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if primary.FirstSOF != 0xc2 || primary.SOSCount < 2 || !primary.HasValidICC || !primary.Is420 {
		t.Fatalf("unexpected progressive primary: %+v", primary)
	}
	gainOffset := bytes.Index(result.Bytes[primary.End:], []byte{0xff, 0xd8})
	if gainOffset < 0 {
		t.Fatal("gain-map JPEG codestream missing")
	}
	gainMap, err := InspectPrimaryJPEG(result.Bytes[primary.End+gainOffset:])
	if err != nil {
		t.Fatal(err)
	}
	if gainMap.FirstSOF != 0xc0 || gainMap.SOSCount != 1 {
		t.Fatalf("gain-map JPEG must remain baseline: %+v", gainMap)
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
	if probe.HDRMax < 768 || probe.HDRHighlights == 0 || probe.HDRMax-probe.HDRMin < 256 {
		t.Fatalf("full HDR decode lacks nontrivial highlights: %+v", probe)
	}
	if probe.HDRClipped*20 > probe.Width*probe.Height {
		t.Fatalf("more than 5%% of full HDR decode is clipped: %+v", probe)
	}
	for _, marker := range [][]byte{
		[]byte("MPF\x00"),
		[]byte("http://ns.adobe.com/xap/1.0/\x00"),
		[]byte("urn:iso:std:iso:ts:21496:-1"),
	} {
		if !bytes.Contains(result.Bytes, marker) {
			t.Fatalf("Ultra HDR output missing metadata marker %q", marker)
		}
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

func TestUltraHDRBackendCancellationTerminatesNativeHelper(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = CreateContext(ctx, input, Options{Size: "2000x0", OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled helper error = %v, want deadline exceeded", err)
	}
}

func TestPinnedLibUltraHDRPreservesProgressiveCompressedPrimary(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Create(input, Options{Size: "320x0", OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}

	helper := helperPath()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "input.jpg")
	basePath := filepath.Join(dir, "progressive-base.jpg")
	if err := os.WriteFile(inPath, input, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(helper, "progressive-base", inPath, basePath, "320x0").CombinedOutput(); err != nil {
		t.Fatalf("progressive base generation failed: %v: %s", err, output)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	// Pinned API-2 prepends JPEG_R container metadata after SOI, then copies the supplied
	// compressed primary from byte 2 onward. This proves no primary entropy/tables transcode.
	if !bytes.Contains(result.Bytes, base[2:]) {
		t.Fatal("JPEG_R output does not preserve supplied progressive primary bytes")
	}
	legacyImage, err := jpeg.Decode(bytes.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	var baseline bytes.Buffer
	if err := jpeg.Encode(&baseline, legacyImage, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if len(base) > baseline.Len()*110/100 {
		t.Fatalf("progressive primary size %d exceeds baseline quality-90 regression bound %d", len(base), baseline.Len()*110/100)
	}
	info, err := InspectPrimaryJPEG(result.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if info.FirstSOF != 0xc2 || info.SOSCount < 2 || !info.HasValidICC || !info.Is420 {
		t.Fatalf("preserved primary properties = %+v", info)
	}
}

func TestUltraHDRBackendAllFixturesProgressiveFullDecode(t *testing.T) {
	for _, name := range []string{"current-photo-1.jpg", "current-photo-2.jpg", "current-photo-3.jpg"} {
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("../../tests/data/hdr", name))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Create(input, Options{Size: "160x0", OriginalName: name, OriginalContentType: "image/jpeg"})
			if err != nil {
				t.Fatal(err)
			}
			info, err := ValidateProgressiveUltraHDR(result.Bytes, "image/jpeg")
			if err != nil {
				t.Fatal(err)
			}
			probe, err := Probe(result.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if info.Width != 160 || !info.HasValidICC || !info.Is420 || probe.GainmapWidth != info.Width || probe.GainmapHeight != info.Height || probe.HDRHighlights == 0 {
				t.Fatalf("progressive/full-decode properties: info=%+v probe=%+v", info, probe)
			}
		})
	}
}

func TestUltraHDRBackendGalleryDimensions(t *testing.T) {
	input, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	for size, want := range map[string][2]int{
		"400x0":  {400, 533},
		"1200x0": {1200, 1600},
		"2000x0": {2000, 2667},
	} {
		t.Run(size, func(t *testing.T) {
			result, err := Create(input, Options{Size: size, OriginalName: "current-photo-1.jpg", OriginalContentType: "image/jpeg"})
			if err != nil {
				t.Fatal(err)
			}
			info, err := ValidateProgressiveUltraHDR(result.Bytes, "image/jpeg")
			if err != nil {
				t.Fatal(err)
			}
			if !info.HasValidICC || !info.Is420 || info.Width != want[0] || info.Height != want[1] {
				t.Fatalf("primary properties = %+v, want dimensions %dx%d, ICC, and 4:2:0", info, want[0], want[1])
			}
			if len(result.Bytes) > info.Width*info.Height*2 {
				t.Fatalf("thumbnail size %d exceeds 2 bytes/pixel regression bound", len(result.Bytes))
			}
		})
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
