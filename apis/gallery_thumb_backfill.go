package apis

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// GalleryThumbBackfillStats summarizes one idempotent backfill pass.
type GalleryThumbBackfillStats struct {
	Generated int
	Skipped   int
	Failed    int
}

// BackfillGalleryThumbs pre-generates the complete versioned variant set for
// published photos. Old generations are retained for rollback and CDN expiry.
func BackfillGalleryThumbs(ctx context.Context, app core.App, workers int) (GalleryThumbBackfillStats, error) {
	if workers < 1 {
		return GalleryThumbBackfillStats{}, errors.New("gallery thumbnail backfill workers must be positive")
	}
	collection, err := app.FindCollectionByNameOrId("photos")
	if errors.Is(err, sql.ErrNoRows) {
		return GalleryThumbBackfillStats{}, nil
	}
	if err != nil {
		return GalleryThumbBackfillStats{}, err
	}

	materializer := newThumbMaterializerFromEnv()
	generate := func(ctx context.Context, record *core.Record) (bool, error) {
		fsys, err := app.NewFilesystem()
		if err != nil {
			return false, err
		}
		defer fsys.Close()

		ready, err := galleryRecordVariantsReady(ctx, fsys, record)
		if err != nil {
			return false, err
		}
		if ready {
			return false, nil
		}
		_, err = materializer.materializeGalleryRecord(ctx, fsys, record)
		return err == nil, err
	}

	var total GalleryThumbBackfillStats
	var firstErr error
	lastID := ""
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		batch, err := app.FindRecordsByFilter(
			collection,
			"published = true && id > {:lastID}",
			"id",
			100,
			0,
			dbx.Params{"lastID": lastID},
		)
		if err != nil {
			return total, err
		}
		if len(batch) == 0 {
			break
		}
		stats, err := runBoundedGalleryBackfill(ctx, batch, workers, generate)
		total.Generated += stats.Generated
		total.Skipped += stats.Skipped
		total.Failed += stats.Failed
		if err != nil && firstErr == nil {
			firstErr = err
		}
		lastID = batch[len(batch)-1].Id
	}
	if firstErr != nil {
		return total, fmt.Errorf("gallery thumbnail backfill completed with failures: %w", firstErr)
	}
	return total, nil
}

func galleryRecordVariantsReady(ctx context.Context, fsys *filesystem.System, record *core.Record) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	filename, err := galleryRecordFilename(record)
	if err != nil {
		return false, err
	}
	manifest, err := fsys.GetReader(galleryHDRReadyPath(record.BaseFilesPath(), filename))
	if errors.Is(err, filesystem.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	manifestData, readErr := io.ReadAll(io.LimitReader(manifest, 1024))
	closeErr := manifest.Close()
	if readErr != nil || closeErr != nil {
		return false, errors.Join(readErr, closeErr)
	}
	if !bytes.Equal(manifestData, galleryHDRReadyManifest()) {
		return false, nil
	}
	for _, size := range galleryHDRThumbSizes {
		path := galleryHDRThumbPath(record.BaseFilesPath(), filename, size)
		attrs, err := fsys.Attributes(path)
		if errors.Is(err, filesystem.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if err := validateGalleryHDRThumbAttrs(attrs); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func runBoundedGalleryBackfill(
	ctx context.Context,
	records []*core.Record,
	workers int,
	generate func(context.Context, *core.Record) (bool, error),
) (GalleryThumbBackfillStats, error) {
	if workers < 1 {
		return GalleryThumbBackfillStats{}, errors.New("gallery thumbnail backfill workers must be positive")
	}
	jobs := make(chan *core.Record)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var stats GalleryThumbBackfillStats
	var firstErr error

	worker := func() {
		defer wg.Done()
		for record := range jobs {
			if err := ctx.Err(); err != nil {
				mu.Lock()
				stats.Failed++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				continue
			}
			generated, err := generate(ctx, record)
			mu.Lock()
			switch {
			case err != nil:
				stats.Failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("photo %s: %w", record.Id, err)
				}
			case generated:
				stats.Generated++
			default:
				stats.Skipped++
			}
			mu.Unlock()
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}

sendLoop:
	for _, record := range records {
		select {
		case jobs <- record:
		case <-ctx.Done():
			mu.Lock()
			firstErr = ctx.Err()
			mu.Unlock()
			break sendLoop
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return stats, fmt.Errorf("gallery thumbnail backfill incomplete (generated=%d skipped=%d failed=%d): %w", stats.Generated, stats.Skipped, stats.Failed, firstErr)
	}
	return stats, nil
}
