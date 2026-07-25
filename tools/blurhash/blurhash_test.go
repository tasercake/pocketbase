package blurhash

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/buckket/go-blurhash/base83"
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

func TestComputeBlurHashUsesMaximumComponents(t *testing.T) {
	data := new(bytes.Buffer)
	if err := jpeg.Encode(data, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}

	hash, err := ComputeBlurHash(data)
	if err != nil {
		t.Fatal(err)
	}

	sizeFlag, err := base83.Decode(hash[:1])
	if err != nil {
		t.Fatal(err)
	}
	if horizontal := sizeFlag%9 + 1; horizontal != 9 {
		t.Fatalf("horizontal components = %d, want 9", horizontal)
	}
	if vertical := sizeFlag/9 + 1; vertical != 9 {
		t.Fatalf("vertical components = %d, want 9", vertical)
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
	for _, name := range []string{"current-photo-1.jpg", "current-photo-2.jpg", "current-photo-3.jpg"} {
		t.Run(name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("..", "..", "tests", "data", "hdr", name))
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
		})
	}
}

func TestComputeBlurHashRejectsCorruptImage(t *testing.T) {
	if _, err := ComputeBlurHash(bytes.NewReader([]byte("not an image"))); err == nil {
		t.Fatal("expected corrupt image error")
	}
}
