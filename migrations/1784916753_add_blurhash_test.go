package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestAddBlurhashFieldAddsTextFieldIdempotently(t *testing.T) {
	app := newTestApp(t)

	photos := core.NewBaseCollection("photos")
	photos.Fields.Add(&core.FileField{Name: "image", MaxSelect: 1})
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	if err := addBlurhashField(app); err != nil {
		t.Fatal(err)
	}
	if err := addBlurhashField(app); err != nil {
		t.Fatal(err)
	}

	photos, err := app.FindCollectionByNameOrId("photos")
	if err != nil {
		t.Fatal(err)
	}
	field, ok := photos.Fields.GetByName("blurhash").(*core.TextField)
	if !ok {
		t.Fatalf("blurhash field = %#v, want *core.TextField", photos.Fields.GetByName("blurhash"))
	}
	if field.Max != 100 {
		t.Fatalf("blurhash field max = %d, want 100", field.Max)
	}
}

func TestAddBlurhashFieldPreservesExistingNonTextField(t *testing.T) {
	app := newTestApp(t)

	photos := core.NewBaseCollection("photos")
	photos.Fields.Add(&core.JSONField{Name: "blurhash"})
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	if err := addBlurhashField(app); err != nil {
		t.Fatal(err)
	}

	photos, err := app.FindCollectionByNameOrId("photos")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := photos.Fields.GetByName("blurhash").(*core.JSONField); !ok {
		t.Fatalf("blurhash field was altered: %#v", photos.Fields.GetByName("blurhash"))
	}
}

func TestAddBlurhashFieldSkipsMissingPhotosCollection(t *testing.T) {
	app := newTestApp(t)
	if err := addBlurhashField(app); err != nil {
		t.Fatal(err)
	}
	if reapply, err := blurhashFieldNeedsMigration(app, nil, ""); err != nil || reapply {
		t.Fatalf("missing photos reapply = %v, %v; want false, nil", reapply, err)
	}
}

func TestBlurhashFieldNeedsMigrationOnlyForMissingField(t *testing.T) {
	app := newTestApp(t)
	photos := core.NewBaseCollection("photos")
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	reapply, err := blurhashFieldNeedsMigration(app, nil, "")
	if err != nil || !reapply {
		t.Fatalf("missing field reapply = %v, %v; want true, nil", reapply, err)
	}

	photos.Fields.Add(&core.JSONField{Name: "blurhash"})
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}
	reapply, err = blurhashFieldNeedsMigration(app, nil, "")
	if err != nil || reapply {
		t.Fatalf("wrong field reapply = %v, %v; want false, nil", reapply, err)
	}
}

func newTestApp(t *testing.T) *core.BaseApp {
	t.Helper()

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	return app
}
