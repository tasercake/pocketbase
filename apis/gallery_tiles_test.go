package apis

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/router"
)

func newGalleryTileTestPhoto(t *testing.T, data []byte, published bool) (*core.BaseApp, *core.Record) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	collection := core.NewBaseCollection("photos")
	collection.Fields.Add(&core.BoolField{Name: "published"}, &core.FileField{Name: "image", MaxSelect: 1})
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.NewFileFromBytes(data, "gallery.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("image", file)
	record.Set("published", published)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return app, record
}

func TestAttachGalleryTilesOnlyForReadyCurrentPublishedImage(t *testing.T) {
	app, record := newGalleryTileTestPhoto(t, smallJPEG(t), true)
	const generation = "abcdef0123456789abcdef0123456789"
	claimed, err := claimGalleryTiles(context.Background(), app, record, record.GetString("image"), generation)
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	e := &core.RequestEvent{App: app, Event: router.Event{Request: httptest.NewRequest("GET", "/", nil)}}
	if err := attachGalleryPhotoURLs(e, record); err != nil {
		t.Fatal(err)
	}
	if record.Get("tiles") != nil {
		t.Fatal("incomplete tile set was exposed")
	}

	// Record the outcome of a successful tile generation without requiring
	// native dependencies on the read path.
	if _, err := app.DB().NewQuery(`UPDATE _gallery_tile_sets
		SET width=4100, height=3200, previewWidth=2000, previewHeight=1561
		WHERE generation={:generation}`).Bind(map[string]any{"generation": generation}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := attachGalleryPhotoURLs(e, record); err != nil {
		t.Fatal(err)
	}
	manifest, ok := record.Get("tiles").(galleryTileURLs)
	prefix := "https://media-cdn.penukonda.me/" + record.BaseFilesPath() + "/tiles/uhdr-dz-v1/" + generation
	if !ok || manifest.TileURL != prefix+"/{z}/{x}_{y}.jpg" || manifest.PreviewURL != prefix+"/preview.jpg" ||
		manifest.Width != 4100 || manifest.Height != 3200 || manifest.TileSize != 512 || manifest.Overlap != 8 {
		t.Fatalf("unexpected ready tile manifest: %+v", manifest)
	}
	for _, update := range []func(){
		func() { record.Set("published", false) },
		func() {
			record.Set("published", true)
			record.Set("image", "replacement.jpg")
		},
	} {
		update()
		if err := attachGalleryPhotoURLs(e, record); err != nil {
			t.Fatal(err)
		}
		if record.Get("tiles") != nil {
			t.Fatal("draft or stale tile set was exposed")
		}
	}
}

func TestClaimGalleryTilesRejectsActiveAndStaleSources(t *testing.T) {
	app, record := newGalleryTileTestPhoto(t, smallJPEG(t), true)
	ctx := context.Background()
	filename := record.GetString("image")
	if ok, err := claimGalleryTiles(ctx, app, record, filename, "first"); err != nil || !ok {
		t.Fatalf("claim current source = %v, %v", ok, err)
	}
	if ok, err := claimGalleryTiles(ctx, app, record, filename, "duplicate"); err != nil || ok {
		t.Fatalf("claim active source = %v, %v", ok, err)
	}

	replacement, err := filesystem.NewFileFromBytes(smallJPEG(t), "replacement.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record.Set("image", replacement)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if ok, err := claimGalleryTiles(ctx, app, record, record.GetString("image"), "replacement"); err != nil || !ok {
		t.Fatalf("claim replacement source = %v, %v", ok, err)
	}
	if ok, err := claimGalleryTiles(ctx, app, record, filename, "stale"); err != nil || ok {
		t.Fatalf("claim stale source = %v, %v", ok, err)
	}
}
