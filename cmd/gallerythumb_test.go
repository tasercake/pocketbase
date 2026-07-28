package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/tests"
)

func TestGalleryThumbBackfillCommandNoCollectionIsIdempotent(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	command := cmd.NewGalleryThumbCommand(app)
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetArgs([]string{"backfill", "--workers=2"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "generated=0 skipped=0 failed=0") {
		t.Fatalf("summary = %q", got)
	}
}

func TestGalleryThumbBackfillCommandRejectsZeroWorkers(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	command := cmd.NewGalleryThumbCommand(app)
	command.SetArgs([]string{"backfill", "--workers=0"})
	if err := command.Execute(); err == nil {
		t.Fatal("zero-worker backfill unexpectedly succeeded")
	}
}
