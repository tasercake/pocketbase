package apis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/list"
	"github.com/spf13/cast"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
)

const galleryMediaCDNBaseURL = "https://media-cdn.penukonda.me"

var galleryHDRThumbSizes = []string{"400x0", "1200x0", "2000x0"}

var sharedThumbMaterializer = newThumbMaterializerFromEnv()

type thumbMaterializer struct {
	sem     *semaphore.Weighted
	pending *singleflight.Group
	maxWait time.Duration
}

func newThumbMaterializerFromEnv() *thumbMaterializer {
	maxWorkers := cast.ToInt64(os.Getenv("PB_THUMBS_MAX_WORKERS"))
	if maxWorkers <= 0 {
		maxWorkers = int64(runtime.NumCPU() + 2)
	}

	maxWait := cast.ToInt64(os.Getenv("PB_THUMBS_MAX_WAIT"))
	if maxWait <= 0 {
		maxWait = 60
	}

	return &thumbMaterializer{
		sem:     semaphore.NewWeighted(maxWorkers),
		pending: new(singleflight.Group),
		maxWait: time.Duration(maxWait) * time.Second,
	}
}

func (m *thumbMaterializer) createThumb(ctx context.Context, fsys *filesystem.System, originalPath string, thumbPath string, opts filesystem.ThumbOptions) error {
	ch := m.pending.DoChan(thumbPath, func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, m.maxWait)
		defer cancel()

		if err := m.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer m.sem.Release(1)

		_, err := fsys.CreateThumbWithOptions(originalPath, thumbPath, opts)
		return nil, err
	})

	res := <-ch
	m.pending.Forget(thumbPath)
	return res.Err
}

func (m *thumbMaterializer) detectHDRSource(fsys *filesystem.System, originalPath string, contentType string) (hdrthumb.Detection, error) {
	r, err := fsys.GetReader(originalPath)
	if err != nil {
		return hdrthumb.Detection{}, err
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return hdrthumb.Detection{}, err
	}

	return hdrthumb.DetectBytes(data, contentType)
}

func attachGalleryPhotoURLs(e *core.RequestEvent, records ...*core.Record) error {
	var needsFilesystem bool
	for _, record := range records {
		if record != nil && record.Collection().Name == "photos" && record.GetBool("published") {
			needsFilesystem = true
			break
		}
	}
	if !needsFilesystem {
		return nil
	}

	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()

	for _, record := range records {
		urls, err := sharedThumbMaterializer.materializeGalleryRecord(e.Request.Context(), fsys, record)
		if err != nil {
			return err
		}
		if urls != nil {
			record.Set("urls", urls)
			record.WithCustomData(true)
		}
	}

	return nil
}

func (m *thumbMaterializer) materializeGalleryRecord(ctx context.Context, fsys *filesystem.System, record *core.Record) (map[string]string, error) {
	if record == nil || record.Collection().Name != "photos" {
		return nil, nil
	}

	// CDN URLs are public, so expose them only for records explicitly published.
	if !record.GetBool("published") {
		return nil, nil
	}

	filename := record.GetString("image")
	if filename == "" {
		files := record.GetStringSlice("image")
		if len(files) > 0 {
			filename = files[0]
		}
	}
	if filename == "" {
		return nil, fmt.Errorf("published photo %q has no image file", record.Id)
	}

	fileField, _ := record.Collection().Fields.GetByName("image").(*core.FileField)
	if fileField == nil {
		return nil, fmt.Errorf("published photo %q is missing image file field", record.Id)
	}
	if fileField.MaxSelect > 1 && len(record.GetStringSlice("image")) != 1 {
		return nil, fmt.Errorf("published photo %q must have exactly one image file", record.Id)
	}

	baseFilesPath := record.BaseFilesPath()
	originalPath := baseFilesPath + "/" + filename

	oAttrs, err := fsys.Attributes(originalPath)
	if err != nil {
		return nil, fmt.Errorf("published photo %q source image is missing: %w", record.Id, err)
	}
	if !list.ExistInSlice(oAttrs.ContentType, imageContentTypes) {
		return nil, fmt.Errorf("published photo %q source file is not a supported image (%s)", record.Id, oAttrs.ContentType)
	}

	detected, err := m.detectHDRSource(fsys, originalPath, oAttrs.ContentType)
	if err != nil {
		return nil, fmt.Errorf("published photo %q HDR detection failed: %w", record.Id, err)
	}
	if detected.Kind == hdrthumb.KindNone {
		return nil, hdrthumb.NewError(hdrthumb.ErrHDRRequired, detected.Kind, filename, strings.Join(galleryHDRThumbSizes, ","), "published gallery photos require an HDR source")
	}
	if detected.Kind != hdrthumb.KindUltraHDRJPEG {
		return nil, hdrthumb.NewError(hdrthumb.ErrUnsupportedHDRKind, detected.Kind, filename, strings.Join(galleryHDRThumbSizes, ","), "only Ultra HDR JPEG gallery thumbnails are currently supported")
	}

	urls := make(map[string]string, len(galleryHDRThumbSizes))
	for _, size := range galleryHDRThumbSizes {
		thumbPath := galleryHDRThumbPath(baseFilesPath, filename, size)
		if exists, _ := fsys.Exists(thumbPath); !exists {
			if err := m.createThumb(ctx, fsys, originalPath, thumbPath, filesystem.ThumbOptions{
				Size:              size,
				HdrEnabled:        true,
				HdrPolicy:         core.FileFieldHdrThumbsPolicyRequire,
				SourceContentType: oAttrs.ContentType,
			}); err != nil {
				return nil, fmt.Errorf("failed to materialize HDR thumbnail %s for published photo %q: %w", size, record.Id, err)
			}
		}

		attrs, err := fsys.Attributes(thumbPath)
		if err != nil {
			return nil, fmt.Errorf("materialized HDR thumbnail %s for published photo %q is missing: %w", size, record.Id, err)
		}
		if attrs.ContentType != "image/jpeg" {
			return nil, fmt.Errorf("materialized HDR thumbnail %s for published photo %q has unexpected content type %q", size, record.Id, attrs.ContentType)
		}

		urls[galleryThumbURLField(size)] = galleryMediaURL(thumbPath)
	}

	return urls, nil
}

func galleryHDRThumbPath(baseFilesPath, filename, size string) string {
	return baseFilesPath + "/thumbs_hdr_" + filename + "/" + size + "_" + filename
}

func galleryThumbURLField(size string) string {
	switch size {
	case "400x0":
		return "thumb400"
	case "1200x0":
		return "thumb1200"
	case "2000x0":
		return "thumb2000"
	default:
		return "thumb" + strings.ReplaceAll(size, "x", "")
	}
}

func galleryMediaURL(objectKey string) string {
	parts := strings.Split(objectKey, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return galleryMediaCDNBaseURL + "/" + strings.Join(parts, "/")
}

func isHDRThumbError(err error) bool {
	var hdrErr *hdrthumb.Error
	return errors.As(err, &hdrErr) || errors.Is(err, hdrthumb.ErrHDRBackendUnavailable) || errors.Is(err, hdrthumb.ErrUnsupportedHDRKind) || errors.Is(err, hdrthumb.ErrHDRGenerationFailed) || errors.Is(err, hdrthumb.ErrHDRRequired)
}
