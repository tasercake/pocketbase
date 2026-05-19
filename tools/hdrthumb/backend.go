package hdrthumb

import (
	"errors"
	"fmt"
)

var (
	ErrHDRBackendUnavailable = errors.New("hdr thumbnail backend unavailable")
	ErrUnsupportedHDRKind    = errors.New("unsupported HDR kind")
	ErrHDRGenerationFailed   = errors.New("HDR thumbnail generation failed")
	ErrHDRRequired           = errors.New("HDR thumbnail required")
)

// Error describes an HDR thumbnail routing/generation failure.
type Error struct {
	Err        error
	HDRKind    Kind
	SourceName string
	ThumbSize  string
	Reason     string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	reason := e.Reason
	if reason == "" && e.Err != nil {
		reason = e.Err.Error()
	}
	return fmt.Sprintf("%s (kind=%s source=%q size=%q)", reason, e.HDRKind, e.SourceName, e.ThumbSize)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewError creates a typed HDR thumbnail error with diagnostic context.
func NewError(err error, kind Kind, sourceName, thumbSize, reason string) *Error {
	return &Error{Err: err, HDRKind: kind, SourceName: sourceName, ThumbSize: thumbSize, Reason: reason}
}

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
