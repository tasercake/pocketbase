package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/blurhash"
	"github.com/spf13/cobra"
)

// NewBlurhashCommand creates commands for maintaining photo BlurHash values.
func NewBlurhashCommand(app core.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "blurhash",
		Short: "Manage photo BlurHash placeholders",
	}
	command.AddCommand(&cobra.Command{
		Use:          "backfill",
		Short:        "Generate missing BlurHashes for stored photos",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			updated, skipped, err := backfillBlurhashes(app)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Blurhash backfill complete: updated=%d skipped=%d\n", updated, skipped)
			return nil
		},
	})
	return command
}

func backfillBlurhashes(app core.App) (updated, skipped int, err error) {
	collection, err := app.FindCollectionByNameOrId("photos")
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if _, ok := collection.Fields.GetByName("blurhash").(*core.TextField); !ok {
		return 0, 0, errors.New("photos.blurhash must be a TextField before backfill")
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		return 0, 0, err
	}
	defer fsys.Close()

	lastID := ""
	for {
		records, err := app.FindRecordsByFilter(
			collection,
			"id > {:lastID}",
			"id",
			100,
			0,
			dbx.Params{"lastID": lastID},
		)
		if err != nil {
			return updated, skipped, err
		}
		if len(records) == 0 {
			return updated, skipped, nil
		}
		lastID = records[len(records)-1].Id

		for _, record := range records {
			if record.GetString("blurhash") != "" {
				continue
			}
			filename := record.GetString("image")
			if filename == "" {
				skipped++
				continue
			}

			r, err := fsys.GetReader(record.BaseFilesPath() + "/" + filename)
			if err != nil {
				app.Logger().Warn("failed to read photo during blurhash backfill", "error", err, "recordId", record.Id)
				skipped++
				continue
			}
			hash, hashErr := blurhash.ComputeBlurHash(r)
			closeErr := r.Close()
			if hashErr != nil || closeErr != nil {
				app.Logger().Warn("failed to generate photo blurhash during backfill", "error", errors.Join(hashErr, closeErr), "recordId", record.Id)
				skipped++
				continue
			}

			didUpdate, err := updateBlurhashIfImageUnchanged(app, collection, record.Id, filename, hash)
			if err != nil {
				app.Logger().Warn("failed to save photo blurhash during backfill", "error", err, "recordId", record.Id)
				skipped++
				continue
			}
			if !didUpdate {
				skipped++
				continue
			}
			updated++
		}
	}
}

// updateBlurhashIfImageUnchanged avoids attaching a hash to a file replacement
// that completed while its predecessor was being decoded. It deliberately uses a
// direct update so a derived field does not alter record recency or run hooks.
func updateBlurhashIfImageUnchanged(app core.App, collection *core.Collection, recordID, image, hash string) (bool, error) {
	result, err := app.DB().Update(collection.Name, dbx.Params{"blurhash": hash}, dbx.And(
		dbx.HashExp{"id": recordID, "image": image},
		dbx.NewExp("([[blurhash]] = '' OR [[blurhash]] IS NULL)"),
	)).Execute()
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
