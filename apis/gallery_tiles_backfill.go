package apis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
)

// GalleryTileBackfillOptions bounds the number of simultaneous native
// decoders. IncludeUnpublished also builds draft assets without exposing URLs.
type GalleryTileBackfillOptions struct {
	Workers            int
	IncludeUnpublished bool
	RecordID           string
}

type GalleryTileBackfillStats struct {
	Generated   int
	Ready       int
	Unsupported int
	Pending     int
	Failed      int
}

// BackfillGalleryTiles materializes immutable Deep Zoom assets ahead of reads.
// A partially failed run can be rerun without replacing completed generations.
func BackfillGalleryTiles(ctx context.Context, app core.App, opts GalleryTileBackfillOptions) (GalleryTileBackfillStats, error) {
	var total GalleryTileBackfillStats
	if opts.Workers < 1 || opts.Workers > 2 {
		return total, errors.New("gallery tile backfill workers must be 1 or 2")
	}
	collection, err := app.FindCollectionByNameOrId("photos")
	if errors.Is(err, sql.ErrNoRows) {
		return total, nil
	}
	if err != nil {
		return total, err
	}

	filter := "id > {:lastId}"
	if !opts.IncludeUnpublished {
		filter += " && published = true"
	}
	if opts.RecordID != "" {
		filter += " && id = {:recordId}"
	}

	var firstErr error
	lastID := ""
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		params := dbx.Params{"lastId": lastID}
		if opts.RecordID != "" {
			params["recordId"] = opts.RecordID
		}
		records, err := app.FindRecordsByFilter(collection, filter, "id", 100, 0, params)
		if err != nil {
			return total, err
		}
		if len(records) == 0 {
			break
		}

		var ready, unsupported, pending atomic.Int64
		stats, err := runBoundedGalleryBackfill(ctx, records, opts.Workers, func(ctx context.Context, record *core.Record) (bool, error) {
			generated, err := materializeGalleryTiles(ctx, app, record)
			if err != nil || generated {
				return generated, err
			}

			var state struct {
				Width       int `db:"width"`
				Unsupported int `db:"unsupported"`
			}
			filename := record.GetString("image")
			if filename == "" {
				unsupported.Add(1)
				return false, nil
			}
			err = app.DB().Select("width", "unsupported").From(galleryTileTable).Where(dbx.HashExp{
				"collectionId": collection.Id, "recordId": record.Id, "filename": filename, "version": galleryTileVersion,
			}).WithContext(ctx).One(&state)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				pending.Add(1)
			case err != nil:
				return false, err
			case state.Unsupported == 1:
				unsupported.Add(1)
			case state.Width > 0:
				ready.Add(1)
			default:
				pending.Add(1)
			}
			return false, nil
		})
		total.Generated += stats.Generated
		total.Failed += stats.Failed
		total.Ready += int(ready.Load())
		total.Unsupported += int(unsupported.Load())
		total.Pending += int(pending.Load())
		if err != nil && firstErr == nil {
			firstErr = err
		}
		lastID = records[len(records)-1].Id
	}
	if firstErr != nil {
		return total, fmt.Errorf("gallery tile backfill completed with failures: %w", firstErr)
	}
	return total, nil
}

func registerGalleryTileBackfill(app core.App) {
	if os.Getenv("PB_GALLERY_TILES_BACKGROUND") == "0" || !hdrthumb.Available() {
		return
	}
	var running atomic.Bool
	app.Cron().MustAdd("__pbGalleryTiles__", "*/10 * * * *", func() {
		if !running.CompareAndSwap(false, true) {
			return
		}
		defer running.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		stats, err := BackfillGalleryTiles(ctx, app, GalleryTileBackfillOptions{Workers: 1})
		if err != nil {
			app.Logger().Warn("Gallery tile background generation incomplete", "error", err, "failed", stats.Failed)
		}
	})
}
