package apis

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestUploadGalleryTilesStopsNewWritesAndWaitsForActiveWrites(t *testing.T) {
	const concurrent = 8
	p := hdrthumb.TilePyramid{Width: 3000, Height: 520, PreviewWidth: 2000, PreviewHeight: 347}
	dir := t.TempDir()
	if err := forEachGalleryTile(p, func(name string, width, height int) error {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		var data bytes.Buffer
		if err := jpeg.Encode(&data, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
			return err
		}
		return os.WriteFile(path, data.Bytes(), 0600)
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 20)
	fail, finish := make(chan struct{}), make(chan struct{})
	sendFailures := sync.OnceFunc(func() { close(fail) })
	finishLastWrite := sync.OnceFunc(func() { close(finish) })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		index := requests.Add(1)
		started <- struct{}{}
		<-fail
		if index == 1 {
			<-finish
			return
		}
		http.Error(w, "upload failed", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	fsys, err := filesystem.NewS3("tiles", "auto", server.URL, "access", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	fsys.SetContext(ctx)

	var uploadErr error
	done := make(chan struct{})
	go func() {
		uploadErr = uploadGalleryTiles(ctx, fsys, "test", dir, p, false)
		close(done)
	}()
	defer func() {
		sendFailures()
		finishLastWrite()
		<-done
	}()
	for i := range concurrent {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatalf("only %d of %d concurrent uploads started", i, concurrent)
		}
	}
	sendFailures()
	select {
	case <-done:
		t.Fatal("upload returned before active writes finished")
	case <-time.After(50 * time.Millisecond):
	}
	finishLastWrite()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("upload did not return after active writes finished")
	}
	if uploadErr == nil || requests.Load() != concurrent {
		t.Fatalf("got %d writes and error %v, want %d writes and an error", requests.Load(), uploadErr, concurrent)
	}
}

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
