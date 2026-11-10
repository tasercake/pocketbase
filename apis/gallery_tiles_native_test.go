//go:build hdr_thumbs

package apis

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestGalleryTilesWaitForThumbnailWorkerPermit(t *testing.T) {
	app, record := newGalleryTileTestPhoto(t, smallJPEG(t), false)
	t.Setenv("HDRTHUMB_HELPER", filepath.Join(t.TempDir(), "missing-helper"))

	sem := sharedThumbMaterializer.sem
	var permits int64
	for sem.TryAcquire(1) {
		permits++
	}
	defer sem.Release(permits)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	generated, err := materializeGalleryTiles(ctx, app, record)
	if generated || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected waiting tile render to time out, got %v, %v", generated, err)
	}
}

func TestGalleryTilesBackfillRetriesFailuresAndExposesOnlyPublishedSets(t *testing.T) {
	source, err := os.ReadFile("../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	primary, err := hdrthumb.InspectPrimaryJPEG(source)
	if err != nil {
		t.Fatal(err)
	}
	// Keep primary and ISO signaling but omit the gain JPEG to cause the
	// native probe to fail after the SQLite lease has been acquired.
	app, record := newGalleryTileTestPhoto(t, source[:primary.End], false)
	ctx := context.Background()
	opts := GalleryTileBackfillOptions{Workers: 1, RecordID: record.Id}
	stats, err := BackfillGalleryTiles(ctx, app, opts)
	if err != nil || stats != (GalleryTileBackfillStats{}) {
		t.Fatalf("backfill unexpectedly attempted unpublished record: %+v, %v", stats, err)
	}
	opts.IncludeUnpublished = true
	stats, err = BackfillGalleryTiles(ctx, app, opts)
	if err == nil || stats.Failed != 1 {
		t.Fatalf("backfill omitted decode failure: %+v, %v", stats, err)
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	filename := record.GetString("image")
	if err := fsys.Upload(source, record.BaseFilesPath()+"/"+filename); err != nil {
		t.Fatal(err)
	}
	stats, err = BackfillGalleryTiles(ctx, app, opts)
	if err != nil || stats.Generated != 1 {
		t.Fatalf("retry failed: %+v, %v", stats, err)
	}
	sets, err := fsys.List(record.BaseFilesPath() + "/tiles/")
	if err != nil || len(sets) != 49 {
		t.Fatalf("expected preview and 48 tiles, got %d objects: %v", len(sets), err)
	}
	attrs, err := fsys.Attributes(sets[0].Key)
	if err != nil || attrs.CacheControl != galleryHDRThumbCacheControl {
		t.Fatalf("tile cache control = %+v, %v", attrs, err)
	}

	e := &core.RequestEvent{App: app, Event: router.Event{Request: httptest.NewRequest("GET", "/", nil)}}
	if err := attachGalleryPhotoURLs(e, record); err != nil || record.Get("tiles") != nil {
		t.Fatalf("draft exposed a complete tile set: %v, %v", record.Get("tiles"), err)
	}
	record.Set("published", true)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := attachGalleryPhotoURLs(e, record); err != nil || record.Get("tiles") == nil {
		t.Fatalf("published photo omitted the complete tile set: %v, %v", record.Get("tiles"), err)
	}
	stats, err = BackfillGalleryTiles(ctx, app, opts)
	if err != nil || stats.Ready != 1 || stats.Generated != 0 {
		t.Fatalf("retry replaced a ready set: %+v, %v", stats, err)
	}
}
