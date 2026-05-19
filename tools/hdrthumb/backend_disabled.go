//go:build !hdr_thumbs

package hdrthumb

// Available reports whether an HDR thumbnail generation backend is available.
func Available() bool {
	return false
}

// Create generates an HDR thumbnail using the configured backend.
func Create(input []byte, opts Options) (Result, error) {
	return Result{}, ErrHDRBackendUnavailable
}
