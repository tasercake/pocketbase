package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// NewGalleryTileCommand creates commands for generating gallery Deep Zoom assets.
func NewGalleryTileCommand(app core.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "gallery-tiles",
		Short: "Manage immutable HDR gallery tiles",
	}
	opts := apis.GalleryTileBackfillOptions{Workers: 1}
	backfill := &cobra.Command{
		Use:          "backfill",
		Short:        "Generate missing HDR previews and Deep Zoom tiles",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			stats, err := apis.BackfillGalleryTiles(ctx, app, opts)
			_, _ = fmt.Fprintf(command.OutOrStdout(),
				"Gallery tile backfill: generated=%d ready=%d unsupported=%d pending=%d failed=%d\n",
				stats.Generated, stats.Ready, stats.Unsupported, stats.Pending, stats.Failed)
			if err == nil && stats.Pending > 0 {
				err = fmt.Errorf("%d tile sets remain pending; rerun after active generation completes", stats.Pending)
			}
			return err
		},
	}
	backfill.Flags().IntVar(&opts.Workers, "workers", 1, "maximum concurrent photo generations (1 or 2)")
	backfill.Flags().BoolVar(&opts.IncludeUnpublished, "include-unpublished", false, "also prepare drafts without exposing their tile manifests")
	backfill.Flags().StringVar(&opts.RecordID, "record", "", "process only this photo record ID")
	command.AddCommand(backfill)
	return command
}
