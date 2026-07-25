package cmd_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/buckket/go-blurhash/base83"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func TestBlurhashBackfillOnlyFillsMissingReadablePhotos(t *testing.T) {
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

	missing := savePhoto(t, app, photos, "missing.jpg", "")
	existing := savePhoto(t, app, photos, "existing.jpg", "preserved")
	unreadable := saveUnreadablePhoto(t, app, photos, "missing-file.jpg")

	command := cmd.NewBlurhashCommand(app)
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetArgs([]string{"backfill"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	missing, err = app.FindRecordById(photos, missing.Id)
	if err != nil {
		t.Fatal(err)
	}
	if missing.GetString("blurhash") == "" {
		t.Fatal("expected backfill to persist a hash")
	}
	existing, err = app.FindRecordById(photos, existing.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := existing.GetString("blurhash"); got != "preserved" {
		t.Fatalf("existing hash = %q, want preserved", got)
	}
	unreadable, err = app.FindRecordById(photos, unreadable.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := unreadable.GetString("blurhash"); got != "" {
		t.Fatalf("unreadable hash = %q, want empty", got)
	}
	if !strings.Contains(output.String(), "updated=1") || !strings.Contains(output.String(), "skipped=1") {
		t.Fatalf("summary = %q", output.String())
	}
}

func TestBlurhashBackfillForceRecomputesExistingHashes(t *testing.T) {
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

	existing := savePhoto(t, app, photos, "existing.jpg", "old-hash")

	command := cmd.NewBlurhashCommand(app)
	command.SetArgs([]string{"backfill", "--force"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	existing, err = app.FindRecordById(photos, existing.Id)
	if err != nil {
		t.Fatal(err)
	}
	hash := existing.GetString("blurhash")
	if hash == "old-hash" {
		t.Fatal("force backfill preserved the old hash")
	}
	sizeFlag, err := base83.Decode(hash[:1])
	if err != nil {
		t.Fatal(err)
	}
	if horizontal := sizeFlag%9 + 1; horizontal != 9 {
		t.Fatalf("horizontal components = %d, want 9", horizontal)
	}
	if vertical := sizeFlag/9 + 1; vertical != 9 {
		t.Fatalf("vertical components = %d, want 9", vertical)
	}
}

func savePhoto(t *testing.T, app core.App, photos *core.Collection, image, hash string) *core.Record {
	t.Helper()
	file, err := filesystem.NewFileFromBytes(blurhashTestJPEG(t), image)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(photos)
	record.Set("image", file)
	record.Set("blurhash", hash)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record
}

func saveUnreadablePhoto(t *testing.T, app core.App, photos *core.Collection, image string) *core.Record {
	t.Helper()
	record := core.NewRecord(photos)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().Update(photos.Name, dbx.Params{"image": image}, dbx.HashExp{"id": record.Id}).Execute(); err != nil {
		t.Fatal(err)
	}
	return record
}

func blurhashTestJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 1, color.RGBA{B: 255, A: 255})
	data := new(bytes.Buffer)
	if err := jpeg.Encode(data, img, nil); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
