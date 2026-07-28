//go:build !hdr_thumbs

package hdrthumb

import "context"

// Available reports whether an HDR thumbnail generation backend is available.
func Available() bool {
	return false
}

// Create generates an HDR thumbnail using the configured backend.
func Create(input []byte, opts Options) (Result, error) {
	return Result{}, ErrHDRBackendUnavailable
}

// CreateContext generates an HDR thumbnail using the configured backend.
func CreateContext(ctx context.Context, input []byte, opts Options) (Result, error) {
	return Result{}, ErrHDRBackendUnavailable
}

// Probe requires the native libultrahdr backend.
func Probe(input []byte) (ProbeResult, error) {
	return ProbeResult{}, ErrHDRBackendUnavailable
}

// ProbeContext requires the native libultrahdr backend.
func ProbeContext(ctx context.Context, input []byte) (ProbeResult, error) {
	return ProbeResult{}, ErrHDRBackendUnavailable
}
