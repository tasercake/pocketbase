package cmd

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestUpdateBlurhashIfImageUnchangedSkipsReplacedImage(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	photos := core.NewBaseCollection("photos")
	photos.Fields.Add(
		&core.FileField{Name: "image", MaxSelect: 1},
		&core.TextField{Name: "blurhash", Max: 200},
	)
	if err := app.Save(photos); err != nil {
		t.Fatal(err)
	}

	record := core.NewRecord(photos)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().Update(photos.Name, dbx.Params{"image": "replacement.jpg"}, dbx.HashExp{"id": record.Id}).Execute(); err != nil {
		t.Fatal(err)
	}

	for _, force := range []bool{false, true} {
		updated, err := updateBlurhashIfImageUnchanged(app, photos, record.Id, "original.jpg", "old-image-hash", force)
		if err != nil {
			t.Fatal(err)
		}
		if updated {
			t.Fatalf("force=%v: updated blurhash for a replacement image", force)
		}

		persisted, err := app.FindRecordById(photos, record.Id)
		if err != nil {
			t.Fatal(err)
		}
		if got := persisted.GetString("blurhash"); got != "" {
			t.Fatalf("force=%v: blurhash = %q, want empty", force, got)
		}
	}
}
