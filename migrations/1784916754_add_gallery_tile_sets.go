package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.SystemMigrations.Register(func(app core.App) error {
		_, err := app.DB().NewQuery(`CREATE TABLE IF NOT EXISTS _gallery_tile_sets (
			collectionId TEXT NOT NULL,
			recordId TEXT NOT NULL,
			filename TEXT NOT NULL,
			version TEXT NOT NULL,
			generation TEXT NOT NULL,
			leaseUntil INTEGER NOT NULL DEFAULT 0,
			unsupported INTEGER NOT NULL DEFAULT 0,
			width INTEGER NOT NULL DEFAULT 0,
			height INTEGER NOT NULL DEFAULT 0,
			previewWidth INTEGER NOT NULL DEFAULT 0,
			previewHeight INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (collectionId, recordId)
		)`).Execute()
		return err
	}, func(app core.App) error {
		_, err := app.DB().DropTable("_gallery_tile_sets").Execute()
		return err
	})
}
