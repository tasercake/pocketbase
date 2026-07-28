//go:build hdr_thumbs

package apis

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase/tools/hdrthumb"
)

func TestGalleryMaterializerGeneratesCompleteImmutableProgressiveSet(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "photo.jpg")
	original, err := os.ReadFile(filepath.Join("..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.Upload(original, record.BaseFilesPath()+"/photo.jpg"); err != nil {
		t.Fatal(err)
	}
	legacySentinel := []byte("retained rollback generation")
	legacyPath := legacyGalleryHDRThumbPath(record.BaseFilesPath(), "photo.jpg", "400x0")
	if err := fsys.Upload(legacySentinel, legacyPath); err != nil {
		t.Fatal(err)
	}

	materializer := newThumbMaterializerFromEnv()
	urls, err := materializer.materializeGalleryRecord(context.Background(), fsys, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != len(galleryHDRThumbSizes) {
		t.Fatalf("exposed URL set = %#v", urls)
	}
	var generated400 []byte
	for _, size := range galleryHDRThumbSizes {
		path := galleryHDRThumbPath(record.BaseFilesPath(), "photo.jpg", size)
		attrs, err := fsys.Attributes(path)
		if err != nil {
			t.Fatal(err)
		}
		if attrs.CacheControl != galleryHDRThumbCacheControl || attrs.Metadata[galleryHDRThumbGenerationMetadata] != galleryHDRThumbGenerationVersion {
			t.Fatalf("%s attrs = %+v", size, attrs)
		}
		if err := validateGalleryHDRThumb(context.Background(), fsys, path, size, attrs); err != nil {
			t.Fatalf("%s validation failed: %v", size, err)
		}
		r, err := fsys.GetReader(path)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(readErr, closeErr)
		}
		info, err := hdrthumb.ValidateProgressiveUltraHDR(data, "image/jpeg")
		if err != nil || info.SOSCount < 2 {
			t.Fatalf("%s primary=%+v err=%v", size, info, err)
		}
		if size == "400x0" {
			generated400 = append([]byte(nil), data...)
		}
	}
	manifest, err := fsys.GetReader(galleryHDRReadyPath(record.BaseFilesPath(), "photo.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := io.ReadAll(manifest)
	manifest.Close()
	if err != nil || !bytes.Equal(manifestData, galleryHDRReadyManifest()) {
		t.Fatalf("readiness manifest=%q err=%v", manifestData, err)
	}
	primary, err := hdrthumb.InspectPrimaryJPEG(generated400)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGalleryHDRThumbBytes(context.Background(), generated400[:primary.End], "400x0"); err == nil {
		t.Fatal("truncated JPEG_R primary unexpectedly passed full validation")
	}
	if err := fsys.Delete(galleryHDRReadyPath(record.BaseFilesPath(), "photo.jpg")); err != nil {
		t.Fatal(err)
	}
	ready, err := galleryRecordVariantsReady(context.Background(), fsys, record)
	if err != nil || ready {
		t.Fatalf("variants exposed without readiness manifest: ready=%v err=%v", ready, err)
	}
	if err := fsys.Upload(galleryHDRReadyManifest(), galleryHDRReadyPath(record.BaseFilesPath(), "photo.jpg")); err != nil {
		t.Fatal(err)
	}
	ready, err = galleryRecordVariantsReady(context.Background(), fsys, record)
	if err != nil || !ready {
		t.Fatalf("complete manifested generation not ready: ready=%v err=%v", ready, err)
	}

	wrongPath := galleryHDRThumbPath(record.BaseFilesPath(), "photo.jpg", "400x0")
	wrongAttrs, err := fsys.Attributes(wrongPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGalleryHDRThumb(context.Background(), fsys, wrongPath, "1200x0", wrongAttrs); err == nil {
		t.Fatal("400-wide object unexpectedly satisfied 1200x0 generation")
	}

	legacy, err := fsys.GetReader(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyBytes, err := io.ReadAll(legacy)
	legacy.Close()
	if err != nil || !bytes.Equal(legacyBytes, legacySentinel) {
		t.Fatalf("legacy rollback object changed: %q err=%v", legacyBytes, err)
	}
	if _, ok := materializer.cachedGalleryURLs(record); !ok {
		t.Fatal("complete validated generation did not populate readiness cache")
	}
}
