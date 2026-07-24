package apis

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/blurhash"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	blurhashCreateHookID = "blurhash_create_photo"
	blurhashUpdateHookID = "blurhash_update_photo"
)

func bindBlurhashHooks(app core.App) {
	app.OnRecordCreateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{
		Id:   blurhashCreateHookID,
		Func: computeRecordBlurhash,
	})
	app.OnRecordUpdateRequest().Bind(&hook.Handler[*core.RecordRequestEvent]{
		Id:   blurhashUpdateHookID,
		Func: computeRecordBlurhash,
	})
}

func computeRecordBlurhash(e *core.RecordRequestEvent) error {
	if e.Record == nil || e.Record.Collection().Name != "photos" {
		return e.Next()
	}
	if _, ok := e.Record.Collection().Fields.GetByName("blurhash").(*core.TextField); !ok {
		e.App.Logger().Warn("photos blurhash field is not a TextField; skipping blurhash generation")
		return e.Next()
	}

	files := e.Record.GetUnsavedFiles("image")
	if len(files) == 0 {
		return e.Next()
	}

	r, err := files[0].Reader.Open()
	if err != nil {
		e.App.Logger().Warn("failed to open photo for blurhash generation", "error", err, "recordId", e.Record.Id)
		return e.Next()
	}
	defer r.Close()

	hash, err := blurhash.ComputeBlurHash(r)
	if err != nil {
		e.App.Logger().Warn("failed to generate photo blurhash", "error", err, "recordId", e.Record.Id)
		return e.Next()
	}

	e.Record.Set("blurhash", hash)
	return e.Next()
}
