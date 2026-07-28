//go:build hdr_thumbs

package filesystem_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase/tools/hdrthumb"

	"github.com/pocketbase/pocketbase/tools/filesystem"
)

type hdrProbeResult struct {
	Width         int  `json:"width"`
	Height        int  `json:"height"`
	GainmapWidth  int  `json:"gainmap_width"`
	GainmapHeight int  `json:"gainmap_height"`
	Metadata      bool `json:"metadata"`
	DecodedWidth  int  `json:"decoded_width"`
	DecodedHeight int  `json:"decoded_height"`
}

func TestFileSystemCreateThumbWithOptionsUltraHDR(t *testing.T) {
	dir := createTestDir(t)
	defer os.RemoveAll(dir)

	fsys, err := filesystem.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()

	original, err := os.ReadFile("../../tests/data/hdr/current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.Upload(original, "current-photo-1.jpg"); err != nil {
		t.Fatal(err)
	}

	const cacheControl = "public, max-age=31536000, immutable"
	attrs, err := fsys.CreateThumbWithOptions("current-photo-1.jpg", "thumbs_hdr_current-photo-1.jpg/320x0_current-photo-1.jpg", filesystem.ThumbOptions{
		Size:              "320x0",
		HdrEnabled:        true,
		HdrPolicy:         "require",
		SourceContentType: "image/jpeg",
		CacheControl:      cacheControl,
		Metadata:          map[string]string{"generation": "test-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attrs.ContentType != "image/jpeg" || attrs.CacheControl != cacheControl || attrs.Metadata["generation"] != "test-v1" {
		t.Fatalf("unexpected immutable object attrs: %+v", attrs)
	}

	r, err := fsys.GetReader("thumbs_hdr_current-photo-1.jpg/320x0_current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	info, err := hdrthumb.ValidateProgressiveUltraHDR(thumb, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if info.FirstSOF != 0xc2 || info.SOSCount < 2 || !info.HasValidICC || !info.Is420 {
		t.Fatalf("unexpected stored primary: %+v", info)
	}
	probe := probeStoredHDRThumb(t, thumb)
	if probe.Width != 320 || probe.Height == 0 || probe.DecodedWidth != probe.Width || probe.DecodedHeight != probe.Height {
		t.Fatalf("unexpected libultrahdr decoded thumbnail dimensions: %+v", probe)
	}
	if !probe.Metadata || probe.GainmapWidth != probe.Width || probe.GainmapHeight != probe.Height {
		t.Fatalf("unexpected libultrahdr gain-map thumbnail metadata/dimensions: %+v", probe)
	}

	recorder := httptest.NewRecorder()
	recorder.Header().Set("Cache-Control", cacheControl)
	request := httptest.NewRequest("GET", "/thumb.jpg", nil)
	if err := fsys.Serve(recorder, request, "thumbs_hdr_current-photo-1.jpg/320x0_current-photo-1.jpg", "thumb.jpg"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), thumb) {
		t.Fatal("directly served bytes were transcoded or stripped")
	}
	if recorder.Header().Get("Cache-Control") != cacheControl {
		t.Fatalf("served cache control = %q", recorder.Header().Get("Cache-Control"))
	}

	r, err = fsys.GetReader("current-photo-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, original) {
		t.Fatal("original file bytes changed")
	}

	rejectedPath := "thumbs_hdr_current-photo-1.jpg/rejected.jpg"
	_, err = fsys.CreateThumbWithOptions("current-photo-1.jpg", rejectedPath, filesystem.ThumbOptions{
		Size:              "64x0",
		HdrEnabled:        true,
		HdrPolicy:         "require",
		SourceContentType: "image/jpeg",
		Validate:          func(context.Context, []byte) error { return errors.New("injected validation failure") },
	})
	if err == nil {
		t.Fatal("pre-upload validator unexpectedly succeeded")
	}
	if exists, _ := fsys.Exists(rejectedPath); exists {
		t.Fatal("failed pre-upload validation wrote final object")
	}
}

func probeStoredHDRThumb(t *testing.T, data []byte) hdrProbeResult {
	t.Helper()
	helper := findHDRThumbHelper(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(helper, "probe", path).CombinedOutput()
	if err != nil {
		t.Fatalf("libultrahdr probe failed: %v: %s", err, string(output))
	}
	var probe hdrProbeResult
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("failed to parse libultrahdr probe %q: %v", output, err)
	}
	return probe
}

func findHDRThumbHelper(t *testing.T) string {
	t.Helper()
	if configured := os.Getenv("HDRTHUMB_HELPER"); configured != "" {
		return configured
	}
	if path, err := exec.LookPath("hdrthumb-helper"); err == nil {
		return path
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(wd, ".deps", "hdrthumb", "bin", "hdrthumb-helper")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	t.Fatal("hdrthumb-helper not found; run scripts/hdrthumb/build-libultrahdr.sh")
	return ""
}
