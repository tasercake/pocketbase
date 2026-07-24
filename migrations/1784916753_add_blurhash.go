package migrations

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.SystemMigrations.Add(&core.Migration{
		Up:               addBlurhashField,
		ReapplyCondition: blurhashFieldNeedsMigration,
	})
}

func blurhashFieldNeedsMigration(app core.App, _ *core.MigrationsRunner, _ string) (bool, error) {
	collection, err := app.FindCollectionByNameOrId("photos")
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if collection.Fields.GetByName("blurhash") != nil {
		return false, nil
	}
	return true, nil
}

func addBlurhashField(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("photos")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	field := collection.Fields.GetByName("blurhash")
	if field != nil {
		if _, ok := field.(*core.TextField); !ok {
			app.Logger().Warn("blurhash field exists on photos but is not a TextField; blurhash will not be persisted")
		}
		return nil
	}

	collection.Fields.Add(&core.TextField{Name: "blurhash", Max: 100})
	return app.Save(collection)
}
