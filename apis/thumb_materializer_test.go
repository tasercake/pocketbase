package apis

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
)

func TestGalleryMediaURLEscapesPathSegments(t *testing.T) {
	got := galleryMediaURL("photos/rec 1/thumbs_hdr_a/b.jpg/400x0_a b.jpg")
	want := "https://media-cdn.penukonda.me/photos/rec%201/thumbs_hdr_a/b.jpg/400x0_a%20b.jpg"
	if got != want {
		t.Fatalf("galleryMediaURL() = %q, want %q", got, want)
	}
}

func TestGalleryMaterializerSkipsUnpublishedRecords(t *testing.T) {
	record := newGalleryTestRecord(false, "photo.jpg")

	urls, err := newThumbMaterializerFromEnv().materializeGalleryRecord(nil, nil, record)
	if err != nil {
		t.Fatal(err)
	}
	if urls != nil {
		t.Fatalf("expected no urls for unpublished record, got %#v", urls)
	}
	if got := record.Get("urls"); got != nil {
		t.Fatalf("expected unpublished record custom urls to remain unset, got %#v", got)
	}
}

func TestGalleryMaterializerRejectsSDRSource(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "photo.jpg")
	if err := fsys.Upload(smallJPEG(t), record.BaseFilesPath()+"/photo.jpg"); err != nil {
		t.Fatal(err)
	}

	materializer := newThumbMaterializerFromEnv()
	urls, err := materializer.materializeGalleryRecord(nil, fsys, record)
	if err == nil {
		t.Fatalf("expected HDR-required error, got urls %#v", urls)
	}
	if !errors.Is(err, hdrthumb.ErrHDRRequired) {
		t.Fatalf("expected ErrHDRRequired, got %v", err)
	}
	if record.Get("urls") != nil {
		t.Fatalf("expected no partial urls after failure, got %#v", record.Get("urls"))
	}
	if _, ok := materializer.cachedGalleryURLs(record); ok {
		t.Fatalf("expected SDR source failure not to populate readiness cache")
	}
}

func TestGalleryMaterializerReturnsExistingHDRThumbURLs(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "current photo.jpg")
	data, err := os.ReadFile(filepath.Join("..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Skipf("HDR fixture unavailable: %v", err)
	}

	// Intentionally upload only the thumbnails, not the original source. The
	// already-materialized cold path should verify the thumbnail bytes themselves
	// and avoid full original-file HDR detection before returning URLs.
	for _, size := range galleryHDRThumbSizes {
		if err := fsys.Upload(data, galleryHDRThumbPath(record.BaseFilesPath(), "current photo.jpg", size)); err != nil {
			t.Fatal(err)
		}
	}

	materializer := newThumbMaterializerFromEnv()
	urls, err := materializer.materializeGalleryRecord(nil, fsys, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 3 {
		t.Fatalf("expected 3 urls, got %#v", urls)
	}
	for _, key := range []string{"thumb400", "thumb1200", "thumb2000"} {
		if urls[key] == "" {
			t.Fatalf("missing %s in %#v", key, urls)
		}
		if !bytes.HasPrefix([]byte(urls[key]), []byte(galleryMediaCDNBaseURL+"/photos_collection/record1/")) {
			t.Fatalf("unexpected %s url shape: %q", key, urls[key])
		}
		if bytes.Contains([]byte(urls[key]), []byte(" ")) {
			t.Fatalf("url was not escaped: %q", urls[key])
		}
	}
	if _, ok := materializer.cachedGalleryURLs(record); !ok {
		t.Fatalf("expected existing verified HDR thumbs to populate readiness cache")
	}
}

func TestGalleryMaterializerCacheHitAvoidsFilesystem(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "cached photo.jpg")
	data, err := os.ReadFile(filepath.Join("..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Skipf("HDR fixture unavailable: %v", err)
	}
	for _, size := range galleryHDRThumbSizes {
		if err := fsys.Upload(data, galleryHDRThumbPath(record.BaseFilesPath(), "cached photo.jpg", size)); err != nil {
			t.Fatal(err)
		}
	}

	materializer := newThumbMaterializerFromEnv()
	want, err := materializer.materializeGalleryRecord(nil, fsys, record)
	if err != nil {
		t.Fatal(err)
	}

	got, err := materializer.materializeGalleryRecord(nil, nil, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got["thumb400"] != want["thumb400"] || got["thumb1200"] != want["thumb1200"] || got["thumb2000"] != want["thumb2000"] {
		t.Fatalf("cache hit urls = %#v, want %#v", got, want)
	}
}

func TestGalleryMaterializerCacheKeyChanges(t *testing.T) {
	materializer := newThumbMaterializerFromEnv()
	record := newGalleryTestRecord(true, "photo-a.jpg")
	materializer.storeGalleryReady(record)
	if _, ok := materializer.cachedGalleryURLs(record); !ok {
		t.Fatalf("expected original record cache hit")
	}

	renamed := newGalleryTestRecord(true, "photo-b.jpg")
	if _, ok := materializer.cachedGalleryURLs(renamed); ok {
		t.Fatalf("expected filename change to miss readiness cache")
	}

	updated := newGalleryTestRecord(true, "photo-a.jpg")
	updated.SetRaw("updated", "2026-05-26 10:00:00.000Z")
	if _, ok := materializer.cachedGalleryURLs(updated); ok {
		t.Fatalf("expected updated timestamp change to miss readiness cache")
	}
}

func TestGalleryMaterializerCacheConcurrentHits(t *testing.T) {
	materializer := newThumbMaterializerFromEnv()
	record := newGalleryTestRecord(true, "parallel photo.jpg")
	materializer.storeGalleryReady(record)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				urls, ok := materializer.cachedGalleryURLs(record)
				if !ok || len(urls) != len(galleryHDRThumbSizes) {
					t.Errorf("expected concurrent cache hit, got ok=%v urls=%#v", ok, urls)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestGalleryMaterializerExistingSDRThumbsDoNotBypassHDRSourceValidation(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "photo.jpg")
	data := smallJPEG(t)
	if err := fsys.Upload(data, record.BaseFilesPath()+"/photo.jpg"); err != nil {
		t.Fatal(err)
	}
	for _, size := range galleryHDRThumbSizes {
		if err := fsys.Upload(data, galleryHDRThumbPath(record.BaseFilesPath(), "photo.jpg", size)); err != nil {
			t.Fatal(err)
		}
	}

	materializer := newThumbMaterializerFromEnv()
	urls, err := materializer.materializeGalleryRecord(nil, fsys, record)
	if err == nil {
		t.Fatalf("expected HDR-required error, got urls %#v", urls)
	}
	if !errors.Is(err, hdrthumb.ErrHDRRequired) {
		t.Fatalf("expected ErrHDRRequired, got %v", err)
	}
	if _, ok := materializer.cachedGalleryURLs(record); ok {
		t.Fatalf("expected invalid existing thumbs not to populate readiness cache")
	}
}

func newGalleryTestRecord(published bool, filename string) *core.Record {
	collection := core.NewBaseCollection("photos")
	collection.Id = "photos_collection"
	collection.Fields.Add(
		&core.BoolField{Name: "published"},
		&core.FileField{Name: "image", MaxSelect: 1},
	)

	record := core.NewRecord(collection)
	record.Id = "record1"
	record.Set("published", published)
	record.Set("image", filename)
	record.SetRaw("updated", "2026-05-26 09:00:00.000Z")
	return record
}

func newLocalTestFS(t *testing.T) (*filesystem.System, func()) {
	t.Helper()
	dir := t.TempDir()
	fsys, err := filesystem.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fsys, func() { fsys.Close() }
}

func smallJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: uint8(x * 100), B: uint8(y * 100), A: 255})
		}
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
