// Package blurhash computes compact image placeholders.
package blurhash

import (
	"image"
	"io"

	blurhash "github.com/buckket/go-blurhash"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register the WebP decoder
)

// ComputeBlurHash decodes, auto-orients, and downsizes an image before
// calculating its 4x3 BlurHash placeholder.
func ComputeBlurHash(r io.Reader) (string, error) {
	img, err := imaging.Decode(r, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}

	return blurhash.Encode(4, 3, resizeForBlurhash(img))
}

func resizeForBlurhash(img image.Image) image.Image {
	return imaging.Fit(img, 100, 100, imaging.Linear)
}
