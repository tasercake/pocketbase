package hdrthumb

import "errors"

var (
	ErrHDRBackendUnavailable = errors.New("hdr thumbnail backend unavailable")
	ErrUnsupportedHDRKind    = errors.New("unsupported HDR kind")
	ErrHDRGenerationFailed   = errors.New("HDR thumbnail generation failed")
	ErrHDRRequired           = errors.New("HDR thumbnail required")
)

// Options configures HDR thumbnail generation.
type Options struct {
	Size                string
	OriginalName        string
	ThumbName           string
	OriginalContentType string
}

// Result contains the generated HDR thumbnail bytes and metadata.
type Result struct {
	ContentType string
	Bytes       []byte
	Evidence    []string
}

// Available reports whether an HDR thumbnail generation backend is available.
func Available() bool {
	return false
}

// Create generates an HDR thumbnail using the configured backend.
func Create(input []byte, opts Options) (Result, error) {
	return Result{}, ErrHDRBackendUnavailable
}
