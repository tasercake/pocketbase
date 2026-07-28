package apis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestGalleryMediaURLEscapesPathSegments(t *testing.T) {
	got := galleryMediaURL("photos/rec 1/thumbs_hdr_a/b.jpg/400x0_a b.jpg")
	want := "https://media-cdn.penukonda.me/photos/rec%201/thumbs_hdr_a/b.jpg/400x0_a%20b.jpg"
	if got != want {
		t.Fatalf("galleryMediaURL() = %q, want %q", got, want)
	}
}

func TestAttachGalleryURLsDoesNotSynchronouslyGenerateMissingCollection(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()

	collection := core.NewBaseCollection("photos")
	collection.Fields.Add(
		&core.BoolField{Name: "published"},
		&core.FileField{Name: "image", MaxSelect: 1},
	)
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.NewFileFromBytes(smallJPEG(t), "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("published", true)
	record.Set("image", file)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	if err := attachGalleryPhotoURLs(&core.RequestEvent{App: app, Event: router.Event{Request: httptest.NewRequest("GET", "/", nil)}}, record); err != nil {
		t.Fatal(err)
	}
	urls, ok := record.Get("urls").(map[string]string)
	if !ok || len(urls) != len(galleryHDRThumbSizes) {
		t.Fatalf("legacy fallback URLs = %#v", record.Get("urls"))
	}
	if bytes.Contains([]byte(urls["thumb400"]), []byte("/"+galleryHDRThumbGenerationVersion+"/")) {
		t.Fatalf("missing generation was exposed: %q", urls["thumb400"])
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	for _, size := range galleryHDRThumbSizes {
		exists, err := fsys.Exists(galleryHDRThumbPath(record.BaseFilesPath(), record.GetString("image"), size))
		if err != nil || exists {
			t.Fatalf("request generated %s synchronously: exists=%v err=%v", size, exists, err)
		}
	}
}

func TestExposeMaterializedGalleryPhotoRequiresUnpublishedState(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()
	collection := core.NewBaseCollection("photos")
	collection.Fields.Add(&core.BoolField{Name: "published"}, &core.FileField{Name: "image", MaxSelect: 1})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("published", false)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := exposeMaterializedGalleryPhoto(app, record); err != nil {
		t.Fatal(err)
	}
	persisted, err := app.FindRecordById(collection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.GetBool("published") {
		t.Fatal("materialized record remained unpublished")
	}
	if err := exposeMaterializedGalleryPhoto(app, record); err == nil {
		t.Fatal("already-published record was exposed twice")
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

func TestGalleryMaterializerRejectsOldBaselineObjectsAtNewGenerationPaths(t *testing.T) {
	fsys, cleanup := newLocalTestFS(t)
	defer cleanup()

	record := newGalleryTestRecord(true, "current photo.jpg")
	data, err := os.ReadFile(filepath.Join("..", "tests", "data", "hdr", "current-photo-1.jpg"))
	if err != nil {
		t.Skipf("HDR fixture unavailable: %v", err)
	}
	for _, size := range galleryHDRThumbSizes {
		if err := fsys.Upload(data, galleryHDRThumbPath(record.BaseFilesPath(), "current photo.jpg", size)); err != nil {
			t.Fatal(err)
		}
	}

	materializer := newThumbMaterializerFromEnv()
	urls, err := materializer.materializeGalleryRecord(context.Background(), fsys, record)
	if err == nil {
		t.Fatalf("expected old baseline generation rejection, got urls %#v", urls)
	}
	if _, ok := materializer.cachedGalleryURLs(record); ok {
		t.Fatal("old baseline objects populated new-generation readiness cache")
	}
}

func TestGalleryMaterializerCacheHitAvoidsFilesystem(t *testing.T) {
	record := newGalleryTestRecord(true, "cached photo.jpg")
	materializer := newThumbMaterializerFromEnv()
	materializer.storeGalleryReady(record)
	want := makeGalleryURLs(record.BaseFilesPath(), "cached photo.jpg")

	got, err := materializer.materializeGalleryRecord(nil, nil, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got["thumb400"] != want["thumb400"] || got["thumb1200"] != want["thumb1200"] || got["thumb2000"] != want["thumb2000"] {
		t.Fatalf("cache hit urls = %#v, want %#v", got, want)
	}
}

func TestGalleryGenerationPathsCacheAndRollbackRetention(t *testing.T) {
	record := newGalleryTestRecord(true, "photo name.jpg")
	path := galleryHDRThumbPath(record.BaseFilesPath(), "photo name.jpg", "400x0")
	legacy := legacyGalleryHDRThumbPath(record.BaseFilesPath(), "photo name.jpg", "400x0")
	if path == legacy || !bytes.Contains([]byte(path), []byte("/"+galleryHDRThumbGenerationVersion+"/")) {
		t.Fatalf("versioned path=%q legacy=%q", path, legacy)
	}
	urls := makeGalleryURLs(record.BaseFilesPath(), "photo name.jpg")
	if !bytes.Contains([]byte(urls["thumb400"]), []byte("/"+galleryHDRThumbGenerationVersion+"/")) {
		t.Fatalf("versioned URL missing generation: %q", urls["thumb400"])
	}
	if bytes.Contains([]byte(urls["thumb400"]), []byte(" ")) {
		t.Fatalf("versioned URL was not escaped: %q", urls["thumb400"])
	}
	readyPath := galleryHDRReadyPath(record.BaseFilesPath(), "photo name.jpg")
	if !bytes.Contains([]byte(readyPath), []byte("/"+galleryHDRThumbGenerationVersion+"/_ready")) ||
		!bytes.Contains(galleryHDRReadyManifest(), []byte(galleryHDRThumbGenerationVersion)) {
		t.Fatalf("readiness path=%q manifest=%q", readyPath, galleryHDRReadyManifest())
	}
	key := galleryReadinessCacheKey(record, "photo name.jpg")
	if !bytes.Contains([]byte(key), []byte(galleryHDRThumbGenerationVersion)) {
		t.Fatalf("readiness key missing generation: %q", key)
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

func TestGalleryMaterializerDoesNotOverwriteInvalidImmutableObjects(t *testing.T) {
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
	if !strings.Contains(err.Error(), "bump generation instead of overwriting") {
		t.Fatalf("expected immutable generation error, got %v", err)
	}
	if _, ok := materializer.cachedGalleryURLs(record); ok {
		t.Fatalf("expected invalid existing thumbs not to populate readiness cache")
	}
}

func TestGalleryBackfillEnumeratesPublishedPhotosAndReportsRetryableFailure(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app.ResetBootstrapState()
	collection := core.NewBaseCollection("photos")
	collection.Fields.Add(
		&core.BoolField{Name: "published"},
		&core.FileField{Name: "image", MaxSelect: 1},
	)
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.NewFileFromBytes(smallJPEG(t), "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("published", true)
	record.Set("image", file)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	stats, err := BackfillGalleryThumbs(context.Background(), app, 1)
	if err == nil || stats.Failed != 1 || stats.Generated != 0 {
		t.Fatalf("SDR backfill stats=%+v err=%v", stats, err)
	}
	if !errors.Is(err, hdrthumb.ErrHDRRequired) {
		t.Fatalf("backfill error = %v, want ErrHDRRequired", err)
	}
}

func TestGalleryBackfillBoundedConcurrencyAndIdempotency(t *testing.T) {
	records := newBackfillTestRecords(12)
	var active atomic.Int32
	var maximum atomic.Int32
	var mu sync.Mutex
	ready := map[string]bool{}
	generate := func(ctx context.Context, record *core.Record) (bool, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if ready[record.Id] {
			return false, nil
		}
		ready[record.Id] = true
		return true, nil
	}

	stats, err := runBoundedGalleryBackfill(context.Background(), records, 3, generate)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Generated != len(records) || stats.Skipped != 0 || stats.Failed != 0 {
		t.Fatalf("first pass stats = %+v", stats)
	}
	if got := maximum.Load(); got > 3 || got < 2 {
		t.Fatalf("maximum concurrency = %d, want 2..3", got)
	}
	stats, err = runBoundedGalleryBackfill(context.Background(), records, 3, generate)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Generated != 0 || stats.Skipped != len(records) || stats.Failed != 0 {
		t.Fatalf("idempotent pass stats = %+v", stats)
	}
}

func TestGalleryBackfillRetryAfterPartialFailure(t *testing.T) {
	records := newBackfillTestRecords(5)
	var mu sync.Mutex
	ready := map[string]bool{}
	failedOnce := false
	generate := func(ctx context.Context, record *core.Record) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if ready[record.Id] {
			return false, nil
		}
		if record.Id == "record-2" && !failedOnce {
			failedOnce = true
			return false, errors.New("injected interruption")
		}
		ready[record.Id] = true
		return true, nil
	}

	first, err := runBoundedGalleryBackfill(context.Background(), records, 2, generate)
	if err == nil || first.Failed != 1 || first.Generated != 4 {
		t.Fatalf("partial pass stats=%+v err=%v", first, err)
	}
	second, err := runBoundedGalleryBackfill(context.Background(), records, 2, generate)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generated != 1 || second.Skipped != 4 || second.Failed != 0 {
		t.Fatalf("retry stats = %+v", second)
	}
}

func TestGalleryBackfillCancellationIsRetryable(t *testing.T) {
	records := newBackfillTestRecords(20)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	generate := func(ctx context.Context, record *core.Record) (bool, error) {
		if calls.Add(1) == 1 {
			cancel()
		}
		return true, nil
	}
	_, err := runBoundedGalleryBackfill(ctx, records, 1, generate)
	if err == nil {
		t.Fatal("cancelled backfill unexpectedly succeeded")
	}
	stats, err := runBoundedGalleryBackfill(context.Background(), records, 2,
		func(context.Context, *core.Record) (bool, error) { return true, nil })
	if err != nil || stats.Generated != len(records) {
		t.Fatalf("retry after cancellation stats=%+v err=%v", stats, err)
	}
}

func newBackfillTestRecords(count int) []*core.Record {
	records := make([]*core.Record, count)
	for i := range records {
		records[i] = newGalleryTestRecord(true, "photo.jpg")
		records[i].Id = fmt.Sprintf("record-%d", i)
	}
	return records
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
