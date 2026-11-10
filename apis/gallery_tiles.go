package apis

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
)

const (
	galleryTileTable        = "_gallery_tile_sets"
	galleryTileVersion      = "uhdr-dz-v1"
	galleryTileSize         = 512
	galleryTileOverlap      = 8
	galleryTilePreviewLimit = 2000
	galleryTileMaxDimension = 16384
	galleryTileMaxPixels    = 100_000_000
	galleryTileMaxSource    = 250 << 20
	galleryTileTimeout      = 15 * time.Minute
)

type galleryTileSet struct {
	CollectionID  string `db:"collectionId"`
	RecordID      string `db:"recordId"`
	Filename      string `db:"filename"`
	Generation    string `db:"generation"`
	Width         int    `db:"width"`
	Height        int    `db:"height"`
	PreviewWidth  int    `db:"previewWidth"`
	PreviewHeight int    `db:"previewHeight"`
}

type galleryTileURLs struct {
	TileURL       string `json:"tileUrl"`
	PreviewURL    string `json:"previewUrl"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	PreviewWidth  int    `json:"previewWidth"`
	PreviewHeight int    `json:"previewHeight"`
	TileSize      int    `json:"tileSize"`
	Overlap       int    `json:"overlap"`
}

func galleryTilePrefix(record *core.Record, generation string) string {
	return record.BaseFilesPath() + "/tiles/" + galleryTileVersion + "/" + generation
}

// The gallery read path only queries committed metadata. It never opens the
// master, contacts object storage, or calls the native tile helper.
func attachGalleryTileURLs(e *core.RequestEvent, records []*core.Record) error {
	var ids []any
	for _, record := range records {
		if record.Collection().Name == "photos" && record.Get("tiles") != nil {
			record.Set("tiles", nil)
		}
		if isPublishedPhotoRecord(record) {
			ids = append(ids, record.Id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var sets []galleryTileSet
	err := e.App.DB().Select("collectionId", "recordId", "filename", "generation", "width", "height", "previewWidth", "previewHeight").
		From(galleryTileTable).WithContext(e.Request.Context()).
		Where(dbx.In("recordId", ids...)).
		AndWhere(dbx.HashExp{"version": galleryTileVersion}).
		AndWhere(dbx.NewExp("width > 0")).All(&sets)
	if err != nil {
		// Preview-only is still usable if tile metadata is unavailable.
		e.App.Logger().Warn("Unable to read gallery tile manifests", "error", err)
		return nil
	}
	byRecord := make(map[[2]string]galleryTileSet, len(sets))
	for _, set := range sets {
		byRecord[[2]string{set.CollectionID, set.RecordID}] = set
	}
	for _, record := range records {
		if !isPublishedPhotoRecord(record) {
			continue
		}
		set, ok := byRecord[[2]string{record.Collection().Id, record.Id}]
		if !ok || set.Filename != record.GetString("image") {
			continue
		}
		prefix := galleryMediaURL(galleryTilePrefix(record, set.Generation))
		record.Set("tiles", galleryTileURLs{
			TileURL: prefix + "/{z}/{x}_{y}.jpg", PreviewURL: prefix + "/preview.jpg",
			Width: set.Width, Height: set.Height, PreviewWidth: set.PreviewWidth, PreviewHeight: set.PreviewHeight,
			TileSize: galleryTileSize, Overlap: galleryTileOverlap,
		})
		record.WithCustomData(true)
	}
	return nil
}

func validGalleryPyramid(p hdrthumb.TilePyramid) bool {
	return p.Width > 0 && p.Height > 0 && p.Width <= galleryTileMaxDimension && p.Height <= galleryTileMaxDimension &&
		p.Width*p.Height <= galleryTileMaxPixels && p.PreviewWidth > 0 && p.PreviewHeight > 0 &&
		p.PreviewWidth <= min(galleryTilePreviewLimit, p.Width) && p.PreviewHeight <= min(galleryTilePreviewLimit, p.Height) &&
		max(p.PreviewWidth, p.PreviewHeight) == min(galleryTilePreviewLimit, max(p.Width, p.Height))
}

// The same Deep Zoom geometry as the native worker. Checking every expected
// object before committing readiness catches partial output and malformed edges.
func forEachGalleryTile(p hdrthumb.TilePyramid, visit func(string, int, int) error) error {
	if !validGalleryPyramid(p) {
		return fmt.Errorf("invalid HDR tile dimensions: %+v", p)
	}
	if err := visit("preview.jpg", p.PreviewWidth, p.PreviewHeight); err != nil {
		return err
	}
	maxLevel := bits.Len(uint(max(p.Width, p.Height) - 1))
	for z := maxLevel; z >= 0; z-- {
		divisor := 1 << (maxLevel - z)
		lw, lh := (p.Width+divisor-1)/divisor, (p.Height+divisor-1)/divisor
		if max(lw, lh) <= galleryTilePreviewLimit {
			break
		}
		for y := 0; y*galleryTileSize < lh; y++ {
			for x := 0; x*galleryTileSize < lw; x++ {
				left, top := max(0, x*galleryTileSize-galleryTileOverlap), max(0, y*galleryTileSize-galleryTileOverlap)
				right, bottom := min(lw, (x+1)*galleryTileSize+galleryTileOverlap), min(lh, (y+1)*galleryTileSize+galleryTileOverlap)
				if err := visit(fmt.Sprintf("%d/%d_%d.jpg", z, x, y), right-left, bottom-top); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func uploadGalleryTiles(fsys *filesystem.System, prefix, outputDir string, p hdrthumb.TilePyramid, hdr bool) error {
	return forEachGalleryTile(p, func(name string, width, height int) error {
		data, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width != width || cfg.Height != height {
			return fmt.Errorf("unexpected JPEG dimensions for %s: %dx%d, want %dx%d", name, cfg.Width, cfg.Height, width, height)
		}
		detection, err := hdrthumb.DetectBytes(data, "image/jpeg")
		if err != nil || (hdr && detection.Kind != hdrthumb.KindUltraHDRJPEG) || (!hdr && detection.Kind != hdrthumb.KindNone) {
			return fmt.Errorf("unexpected dynamic range for tile %s: %v", name, detection.Kind)
		}
		return fsys.UploadWithOptions(data, prefix+"/"+name, &blob.WriterOptions{
			ContentType: "image/jpeg", CacheControl: galleryHDRThumbCacheControl,
		})
	})
}

// Claim a unique generation per image. A timed-out process can be retried, and
// another PocketBase process cannot publish or overwrite that generation.
func claimGalleryTiles(ctx context.Context, app core.App, record *core.Record, filename, generation string) (bool, error) {
	now := time.Now()
	result, err := app.DB().NewQuery(`INSERT INTO _gallery_tile_sets
		(collectionId, recordId, filename, version, generation, leaseUntil)
		SELECT {:collection}, {:record}, {:filename}, {:version}, {:generation}, {:until}
		FROM {{photos}} WHERE [[id]]={:record} AND [[image]]={:filename}
		ON CONFLICT(collectionId, recordId) DO UPDATE SET
			filename=excluded.filename, version=excluded.version, generation=excluded.generation, leaseUntil=excluded.leaseUntil,
			width=0, height=0, previewWidth=0, previewHeight=0, unsupported=0
		WHERE filename != excluded.filename OR version != excluded.version OR
			(width = 0 AND unsupported = 0 AND leaseUntil <= {:now})`).
		Bind(dbx.Params{"collection": record.Collection().Id, "record": record.Id, "filename": filename,
			"version": galleryTileVersion, "generation": generation, "now": now.Unix(), "until": now.Add(galleryTileTimeout + time.Minute).Unix()}).
		WithContext(ctx).Execute()
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func updateGalleryTileSet(ctx context.Context, app core.App, record *core.Record, generation string, data dbx.Params) (bool, error) {
	n, err := app.DB().Update(galleryTileTable, data, dbx.HashExp{
		"collectionId": record.Collection().Id, "recordId": record.Id, "generation": generation,
	}).WithContext(ctx).Execute()
	if err != nil {
		return false, err
	}
	affected, err := n.RowsAffected()
	return affected == 1, err
}

func materializeGalleryTiles(ctx context.Context, app core.App, record *core.Record) (bool, error) {
	filename, err := galleryRecordFilename(record)
	if err != nil {
		if record.GetString("image") == "" {
			return false, nil
		}
		return false, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return false, err
	}
	generation := hex.EncodeToString(token[:])
	ctx, cancel := context.WithTimeout(ctx, galleryTileTimeout)
	defer cancel()
	claimed, err := claimGalleryTiles(ctx, app, record, filename, generation)
	if err != nil || !claimed {
		return false, err
	}

	prefix := galleryTilePrefix(record, generation)
	complete, uploading := false, false
	defer func() {
		if complete {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if uploading {
			fsys, err := app.NewFilesystem()
			if err == nil {
				fsys.SetContext(cleanup)
				err = errors.Join(errors.Join(fsys.DeletePrefix(prefix+"/")...), fsys.Close())
			}
			if err != nil {
				app.Logger().Warn("Incomplete gallery tile generation cleanup failed", "recordId", record.Id, "error", err)
			}
		}
		if _, err := updateGalleryTileSet(cleanup, app, record, generation, dbx.Params{"leaseUntil": 0}); err != nil {
			app.Logger().Warn("Gallery tile lease release failed", "recordId", record.Id, "error", err)
		}
	}()

	fsys, err := app.NewFilesystem()
	if err != nil {
		return false, err
	}
	defer fsys.Close()
	fsys.SetContext(ctx)
	r, err := fsys.GetReader(record.BaseFilesPath() + "/" + filename)
	if err != nil {
		return false, err
	}
	contentType := r.ContentType()
	source, readErr := io.ReadAll(io.LimitReader(r, galleryTileMaxSource+1))
	if err := errors.Join(readErr, r.Close()); err != nil {
		return false, err
	}
	if len(source) > galleryTileMaxSource {
		return false, fmt.Errorf("source exceeds %d MiB", galleryTileMaxSource>>20)
	}
	detection, err := hdrthumb.DetectBytes(source, contentType)
	if err != nil {
		return false, err
	}
	hdr := detection.Kind == hdrthumb.KindUltraHDRJPEG || detection.Kind == hdrthumb.KindAdobeGainMapJPEG
	if !hdr && (detection.Kind != hdrthumb.KindNone || detection.ContentType != "image/jpeg") {
		complete, err = updateGalleryTileSet(ctx, app, record, generation, dbx.Params{"leaseUntil": 0, "unsupported": 1})
		if err == nil {
			app.Logger().Info("Gallery tiles unavailable for source format", "recordId", record.Id, "kind", detection.Kind)
		}
		return false, err
	}

	dir, err := os.MkdirTemp("", "pocketbase-gallery-tiles-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "source.jpg")
	if err := os.WriteFile(input, source, 0600); err != nil {
		return false, err
	}
	pyramid, err := hdrthumb.RenderTilesContext(ctx, input, dir, hdr)
	if err != nil {
		return false, err
	}
	uploading = true
	if err := uploadGalleryTiles(fsys, prefix, dir, pyramid, hdr); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// If the photo was replaced while generating tiles, never associate the
	// old generation with the new source (even for simultaneous CLI/cron).
	err = app.RunInTransaction(func(tx core.App) error {
		current, err := tx.FindRecordById(record.Collection().Id, record.Id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.GetString("image") != filename {
			return nil
		}
		complete, err = updateGalleryTileSet(ctx, tx, record, generation, dbx.Params{
			"leaseUntil": 0, "width": pyramid.Width, "height": pyramid.Height,
			"previewWidth": pyramid.PreviewWidth, "previewHeight": pyramid.PreviewHeight,
		})
		return err
	})
	if err != nil {
		complete = false
	}
	return complete, err
}
