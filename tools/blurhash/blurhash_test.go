package blurhash

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeBlurHashJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 80), B: 100, A: 255})
		}
	}

	data := new(bytes.Buffer)
	if err := jpeg.Encode(data, img, nil); err != nil {
		t.Fatal(err)
	}

	hash, err := ComputeBlurHash(data)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected a blurhash")
	}
}

func TestResizeForBlurhashBoundsBothDimensions(t *testing.T) {
	resized := resizeForBlurhash(image.NewRGBA(image.Rect(0, 0, 1, 10000)))
	if resized.Bounds().Dx() > 100 || resized.Bounds().Dy() > 100 {
		t.Fatalf("resized dimensions = %dx%d, want each at most 100", resized.Bounds().Dx(), resized.Bounds().Dy())
	}
}

func TestComputeBlurHashWebP(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sample.webp"))
	if err != nil {
		t.Fatal(err)
	}

	hash, err := ComputeBlurHash(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected a blurhash")
	}
}

func TestComputeBlurHashUltraHDRJPEG(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	hash, err := ComputeBlurHash(file)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected a blurhash")
	}
}

func TestComputeBlurHashRejectsCorruptImage(t *testing.T) {
	if _, err := ComputeBlurHash(bytes.NewReader([]byte("not an image"))); err == nil {
		t.Fatal("expected corrupt image error")
	}
}
