package apis

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func TestBlurhashCreateHookPersistsAndExportsHash(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	bindBlurhashHooks(app)

	image, err := filesystem.NewFileFromBytes(smallJPEG(t), "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(photos)
	record.Set("image", image)

	if err := triggerBlurhashCreate(app, photos, record); err != nil {
		t.Fatal(err)
	}

	persisted, err := app.FindRecordById(photos, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.GetString("blurhash") == "" {
		t.Fatal("expected persisted blurhash")
	}
	if got := persisted.PublicExport()["blurhash"]; got != persisted.GetString("blurhash") {
		t.Fatalf("public blurhash = %#v, want %q", got, persisted.GetString("blurhash"))
	}
}

func TestBlurhashJSONAPIExposesPersistedHash(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	photos.ListRule = types.Pointer("")
	photos.ViewRule = types.Pointer("")
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(photos)
	record.Set("blurhash", "LEHV6nWB2yk8pyo0adR*.7kCMdnj")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	router, err := NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/collections/photos/records/"+record.Id, nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), `"blurhash":"LEHV6nWB2yk8pyo0adR*.7kCMdnj"`; !strings.Contains(got, want) {
		t.Fatalf("response = %s, want %s", got, want)
	}
}

func TestBlurhashUpdateHookKeepsExistingHashWithoutNewImage(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	bindBlurhashHooks(app)

	record := core.NewRecord(photos)
	record.Set("blurhash", "existing-hash")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	event := &core.RecordRequestEvent{
		RequestEvent: &core.RequestEvent{App: app},
		Record:       record,
	}
	if err := app.OnRecordUpdateRequest().Trigger(event, func(e *core.RecordRequestEvent) error {
		return app.Save(e.Record)
	}); err != nil {
		t.Fatal(err)
	}

	persisted, err := app.FindRecordById(photos, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.GetString("blurhash"); got != "existing-hash" {
		t.Fatalf("blurhash = %q, want existing hash", got)
	}
}

func TestBlurhashUpdateHookReplacesHashForNewImage(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	bindBlurhashHooks(app)

	record := core.NewRecord(photos)
	record.Set("blurhash", "old-hash")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	image, err := filesystem.NewFileFromBytes(smallJPEG(t), "replacement.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record.Set("image", image)

	event := &core.RecordRequestEvent{
		RequestEvent: &core.RequestEvent{App: app},
		Record:       record,
	}
	if err := app.OnRecordUpdateRequest().Trigger(event, func(e *core.RecordRequestEvent) error {
		return app.Save(e.Record)
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := app.FindRecordById(photos, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.GetString("blurhash"); got == "" || got == "old-hash" {
		t.Fatalf("blurhash = %q, want replacement hash", got)
	}
}

func TestBlurhashMultipartPatchClearsOldHashForCorruptReplacement(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	photos.UpdateRule = types.Pointer("")
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	oldImage, err := filesystem.NewFileFromBytes(smallJPEG(t), "original.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(photos)
	record.Set("image", oldImage)
	record.Set("blurhash", "old-hash")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	oldFilename := record.GetString("image")

	router, err := NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	body := new(bytes.Buffer)
	form := multipart.NewWriter(body)
	file, err := form.CreateFormFile("image", "replacement.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("not an image")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/collections/photos/records/"+record.Id, body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	persisted, err := app.FindRecordById(photos, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.GetString("image"); got == "" || got == oldFilename {
		t.Fatalf("image = %q, want persisted replacement distinct from %q", got, oldFilename)
	}
	if got := persisted.GetString("blurhash"); got != "" {
		t.Fatalf("blurhash = %q, want empty for corrupt replacement", got)
	}
}

func TestBlurhashCreateHookAllowsCorruptImage(t *testing.T) {
	app, photos := newBlurhashTestApp(t)
	bindBlurhashHooks(app)

	image, err := filesystem.NewFileFromBytes([]byte("not an image"), "broken.jpg")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(photos)
	record.Set("image", image)

	if err := triggerBlurhashCreate(app, photos, record); err != nil {
		t.Fatalf("corrupt image should not reject upload: %v", err)
	}
	persisted, err := app.FindRecordById(photos, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.GetString("blurhash"); got != "" {
		t.Fatalf("blurhash = %q, want empty", got)
	}
}

func newBlurhashTestApp(t *testing.T) (*core.BaseApp, *core.Collection) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })

	photos := core.NewBaseCollection("photos")
	photos.Fields.Add(
		&core.FileField{Name: "image", MaxSelect: 1},
		&core.TextField{Name: "blurhash", Max: 100},
	)
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}
	return app, photos
}

func triggerBlurhashCreate(app *core.BaseApp, photos *core.Collection, record *core.Record) error {
	event := &core.RecordRequestEvent{
		RequestEvent: &core.RequestEvent{App: app},
		Record:       record,
	}
	return app.OnRecordCreateRequest().Trigger(event, func(e *core.RecordRequestEvent) error {
		return app.Save(e.Record)
	})
}
