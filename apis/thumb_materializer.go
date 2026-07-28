package apis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
	"github.com/pocketbase/pocketbase/tools/hdrthumb"
	"github.com/pocketbase/pocketbase/tools/list"
	"github.com/spf13/cast"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"
)

const (
	galleryMediaCDNBaseURL            = "https://media-cdn.penukonda.me"
	galleryHDRThumbGenerationVersion  = "uhdr-pjpeg-v1"
	galleryHDRThumbCacheControl       = "public, max-age=31536000, immutable"
	galleryHDRThumbGenerationMetadata = "pocketbase-thumb-generation"
)

var galleryHDRThumbSizes = []string{"400x0", "1200x0", "2000x0"}

var sharedThumbMaterializer = newThumbMaterializerFromEnv()

type thumbMaterializer struct {
	sem            *semaphore.Weighted
	pending        *singleflight.Group
	maxWait        time.Duration
	readinessCache sync.Map
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
	ctx, cancel := context.WithTimeout(ctx, m.maxWait)
	defer cancel()
	opts.Context = ctx
	ch := m.pending.DoChan(thumbPath, func() (any, error) {
		if err := m.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer m.sem.Release(1)

		_, err := fsys.CreateThumbWithOptions(originalPath, thumbPath, opts)
		return nil, err
	})

	var res singleflight.Result
	select {
	case res = <-ch:
	case <-ctx.Done():
		// Wait for CommandContext/writer cancellation before caller closes fsys.
		res = <-ch
		if res.Err == nil {
			res.Err = ctx.Err()
		}
	}
	m.pending.Forget(thumbPath)
	return res.Err
}

func (m *thumbMaterializer) detectHDRSource(fsys *filesystem.System, originalPath string, contentType string) (hdrthumb.Detection, error) {
	return detectHDRObject(fsys, originalPath, contentType)
}

func detectHDRObject(fsys *filesystem.System, path string, contentType string) (hdrthumb.Detection, error) {
	r, err := fsys.GetReader(path)
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

func materializeGalleryPhotoOnWrite(e *core.RequestEvent, record *core.Record) error {
	if !isPublishedPhotoRecord(record) {
		return nil
	}
	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	urls, err := sharedThumbMaterializer.materializeGalleryRecord(e.Request.Context(), fsys, record)
	if err != nil {
		return err
	}
	if urls != nil {
		record.Set("urls", urls)
		record.WithCustomData(true)
	}
	return nil
}

func exposeMaterializedGalleryPhoto(app core.App, record *core.Record) error {
	result, err := app.DB().Update(
		record.Collection().Name,
		dbx.Params{"published": true},
		dbx.HashExp{"id": record.Id, "published": false},
	).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("photo %q publication state changed during materialization", record.Id)
	}
	return nil
}

func attachGalleryPhotoURLs(e *core.RequestEvent, records ...*core.Record) error {
	var misses []*core.Record
	for _, record := range records {
		if !isPublishedPhotoRecord(record) {
			continue
		}
		if urls, ok := sharedThumbMaterializer.cachedGalleryURLs(record); ok {
			record.Set("urls", urls)
			record.WithCustomData(true)
			continue
		}
		misses = append(misses, record)
	}
	if len(misses) == 0 {
		return nil
	}

	fsys, err := e.App.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()

	for _, record := range misses {
		ready, err := galleryRecordVariantsReady(e.Request.Context(), fsys, record)
		if err != nil {
			return err
		}
		filename, err := galleryRecordFilename(record)
		if err != nil {
			return err
		}
		var urls map[string]string
		if ready {
			sharedThumbMaterializer.storeGalleryReady(record)
			urls = makeGalleryURLs(record.BaseFilesPath(), filename)
		} else {
			// Rollout fallback: reads never synchronously rebuild a gallery page.
			// Backfill creates and validates all new variants before this switches.
			urls = makeLegacyGalleryURLs(record.BaseFilesPath(), filename)
		}
		record.Set("urls", urls)
		record.WithCustomData(true)
	}

	return nil
}

func (m *thumbMaterializer) materializeGalleryRecord(ctx context.Context, fsys *filesystem.System, record *core.Record) (map[string]string, error) {
	if !isPublishedPhotoRecord(record) {
		return nil, nil
	}
	if urls, ok := m.cachedGalleryURLs(record); ok {
		return urls, nil
	}

	filename, err := galleryRecordFilename(record)
	if err != nil {
		return nil, err
	}
	baseFilesPath := record.BaseFilesPath()
	originalPath := baseFilesPath + "/" + filename

	urls := makeGalleryURLs(baseFilesPath, filename)
	badThumbs := make(map[string]struct{})
	allExistingGood := true
	for _, size := range galleryHDRThumbSizes {
		thumbPath := galleryHDRThumbPath(baseFilesPath, filename, size)
		attrs, err := fsys.Attributes(thumbPath)
		if err != nil {
			if errors.Is(err, filesystem.ErrNotFound) {
				badThumbs[size] = struct{}{}
				allExistingGood = false
				continue
			}
			return nil, fmt.Errorf("failed to inspect HDR thumbnail %s for published photo %q: %w", size, record.Id, err)
		}
		if err := validateGalleryHDRThumb(ctx, fsys, thumbPath, size, attrs); err != nil {
			return nil, fmt.Errorf("immutable gallery thumbnail %s for published photo %q is invalid; bump generation instead of overwriting: %w", size, record.Id, err)
		}
	}
	if allExistingGood {
		if err := fsys.Upload(galleryHDRReadyManifest(), galleryHDRReadyPath(baseFilesPath, filename)); err != nil {
			return nil, fmt.Errorf("failed to publish gallery generation readiness for photo %q: %w", record.Id, err)
		}
		m.storeGalleryReady(record)
		return urls, nil
	}

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

	for _, size := range galleryHDRThumbSizes {
		if _, ok := badThumbs[size]; !ok {
			continue
		}
		thumbPath := galleryHDRThumbPath(baseFilesPath, filename, size)
		if err := m.createThumb(ctx, fsys, originalPath, thumbPath, filesystem.ThumbOptions{
			Size:              size,
			HdrEnabled:        true,
			HdrPolicy:         core.FileFieldHdrThumbsPolicyRequire,
			SourceContentType: oAttrs.ContentType,
			CacheControl:      galleryHDRThumbCacheControl,
			Context:           ctx,
			Immutable:         true,
			Validate: func(validateCtx context.Context, data []byte) error {
				return validateGalleryHDRThumbBytes(validateCtx, data, size)
			},
			Metadata: map[string]string{
				galleryHDRThumbGenerationMetadata: galleryHDRThumbGenerationVersion,
			},
		}); err != nil {
			return nil, fmt.Errorf("failed to materialize HDR thumbnail %s for published photo %q: %w", size, record.Id, err)
		}
	}

	for _, size := range galleryHDRThumbSizes {
		thumbPath := galleryHDRThumbPath(baseFilesPath, filename, size)
		attrs, err := fsys.Attributes(thumbPath)
		if err != nil {
			return nil, fmt.Errorf("materialized HDR thumbnail %s for published photo %q is missing: %w", size, record.Id, err)
		}
		if err := validateGalleryHDRThumb(ctx, fsys, thumbPath, size, attrs); err != nil {
			return nil, fmt.Errorf("published photo %q materialized HDR thumbnail %s failed generation validation: %w", record.Id, size, err)
		}
	}
	if err := fsys.Upload(galleryHDRReadyManifest(), galleryHDRReadyPath(baseFilesPath, filename)); err != nil {
		return nil, fmt.Errorf("failed to publish gallery generation readiness for photo %q: %w", record.Id, err)
	}

	m.storeGalleryReady(record)
	return urls, nil
}

func isPublishedPhotoRecord(record *core.Record) bool {
	return record != nil && record.Collection().Name == "photos" && record.GetBool("published")
}

func galleryRecordFilename(record *core.Record) (string, error) {
	filename := record.GetString("image")
	if filename == "" {
		files := record.GetStringSlice("image")
		if len(files) > 0 {
			filename = files[0]
		}
	}
	if filename == "" {
		return "", fmt.Errorf("published photo %q has no image file", record.Id)
	}

	fileField, _ := record.Collection().Fields.GetByName("image").(*core.FileField)
	if fileField == nil {
		return "", fmt.Errorf("published photo %q is missing image file field", record.Id)
	}
	if fileField.MaxSelect > 1 && len(record.GetStringSlice("image")) != 1 {
		return "", fmt.Errorf("published photo %q must have exactly one image file", record.Id)
	}

	return filename, nil
}

func (m *thumbMaterializer) cachedGalleryURLs(record *core.Record) (map[string]string, bool) {
	filename, err := galleryRecordFilename(record)
	if err != nil {
		return nil, false
	}
	if _, ok := m.readinessCache.Load(galleryReadinessCacheKey(record, filename)); !ok {
		return nil, false
	}
	return makeGalleryURLs(record.BaseFilesPath(), filename), true
}

func (m *thumbMaterializer) storeGalleryReady(record *core.Record) {
	filename, err := galleryRecordFilename(record)
	if err != nil {
		return
	}
	m.readinessCache.Store(galleryReadinessCacheKey(record, filename), struct{}{})
}

func galleryReadinessCacheKey(record *core.Record, filename string) string {
	return strings.Join([]string{
		record.Collection().Id,
		record.Collection().Name,
		record.Id,
		filename,
		record.GetString("updated"),
		strings.Join(galleryHDRThumbSizes, ","),
		galleryHDRThumbGenerationVersion,
	}, "\x00")
}

func makeGalleryURLs(baseFilesPath, filename string) map[string]string {
	return makeGalleryURLsForPath(baseFilesPath, filename, galleryHDRThumbPath)
}

func makeLegacyGalleryURLs(baseFilesPath, filename string) map[string]string {
	return makeGalleryURLsForPath(baseFilesPath, filename, legacyGalleryHDRThumbPath)
}

func makeGalleryURLsForPath(baseFilesPath, filename string, pathFor func(string, string, string) string) map[string]string {
	urls := make(map[string]string, len(galleryHDRThumbSizes))
	for _, size := range galleryHDRThumbSizes {
		urls[galleryThumbURLField(size)] = galleryMediaURL(pathFor(baseFilesPath, filename, size))
	}
	return urls
}

func galleryHDRThumbPath(baseFilesPath, filename, size string) string {
	return baseFilesPath + "/thumbs_hdr_" + filename + "/" + galleryHDRThumbGenerationVersion + "/" + size + "_" + filename
}

func legacyGalleryHDRThumbPath(baseFilesPath, filename, size string) string {
	return baseFilesPath + "/thumbs_hdr_" + filename + "/" + size + "_" + filename
}

func galleryHDRReadyPath(baseFilesPath, filename string) string {
	return baseFilesPath + "/thumbs_hdr_" + filename + "/" + galleryHDRThumbGenerationVersion + "/_ready"
}

func galleryHDRReadyManifest() []byte {
	return []byte(galleryHDRThumbGenerationVersion + "\n" + strings.Join(galleryHDRThumbSizes, ",") + "\n")
}

func validateGalleryHDRThumbAttrs(attrs *blob.Attributes) error {
	if attrs.ContentType != "image/jpeg" {
		return fmt.Errorf("unexpected content type %q", attrs.ContentType)
	}
	if attrs.CacheControl != galleryHDRThumbCacheControl {
		return fmt.Errorf("unexpected cache control %q", attrs.CacheControl)
	}
	if attrs.Metadata[galleryHDRThumbGenerationMetadata] != galleryHDRThumbGenerationVersion {
		return fmt.Errorf("missing generation metadata %q", galleryHDRThumbGenerationVersion)
	}
	return nil
}

func validateGalleryHDRThumb(ctx context.Context, fsys *filesystem.System, thumbPath, expectedSize string, attrs *blob.Attributes) error {
	if err := validateGalleryHDRThumbAttrs(attrs); err != nil {
		return err
	}
	r, err := fsys.GetReader(thumbPath)
	if err != nil {
		return err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return validateGalleryHDRThumbBytes(ctx, data, expectedSize)
}

func validateGalleryHDRThumbBytes(ctx context.Context, data []byte, expectedSize string) error {
	info, err := hdrthumb.ValidateProgressiveUltraHDR(data, "image/jpeg")
	if err != nil {
		return err
	}
	if !info.HasValidICC {
		return errors.New("progressive Ultra HDR primary is missing a complete ICC profile")
	}
	if !info.Is420 {
		return errors.New("progressive Ultra HDR primary is not 4:2:0")
	}
	widthText, _, ok := strings.Cut(expectedSize, "x")
	width, err := strconv.Atoi(widthText)
	if !ok || err != nil || width <= 0 {
		return fmt.Errorf("invalid gallery thumbnail size %q", expectedSize)
	}
	if info.Width != width {
		return fmt.Errorf("primary width %d does not match generation size %s", info.Width, expectedSize)
	}
	for name, marker := range map[string][]byte{
		"MPF":       []byte("MPF\x00"),
		"XMP":       []byte("http://ns.adobe.com/xap/1.0/\x00"),
		"ISO 21496": []byte("urn:iso:std:iso:ts:21496:-1"),
	} {
		if !bytes.Contains(data, marker) {
			return fmt.Errorf("Ultra HDR container is missing %s metadata", name)
		}
	}
	probe, err := hdrthumb.ProbeContext(ctx, data)
	if err != nil {
		return err
	}
	if probe.Width != info.Width || probe.Height != info.Height ||
		probe.DecodedWidth != info.Width || probe.DecodedHeight != info.Height ||
		probe.GainmapWidth != info.Width || probe.GainmapHeight != info.Height {
		return fmt.Errorf("Ultra HDR rendition geometry mismatch: primary=%dx%d probe=%+v", info.Width, info.Height, probe)
	}
	if probe.HDRMax < 768 || probe.HDRHighlights == 0 || probe.HDRMax-probe.HDRMin < 256 {
		return fmt.Errorf("Ultra HDR decode lacks nontrivial highlights: %+v", probe)
	}
	if probe.HDRClipped*20 > probe.Width*probe.Height {
		return fmt.Errorf("more than 5%% of Ultra HDR decode is clipped: %+v", probe)
	}
	return nil
}

func isGalleryHDRThumbSize(size string) bool {
	for _, candidate := range galleryHDRThumbSizes {
		if size == candidate {
			return true
		}
	}
	return false
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
