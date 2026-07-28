package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// NewGalleryThumbCommand creates commands for versioned gallery thumbnail maintenance.
func NewGalleryThumbCommand(app core.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "gallery-thumbs",
		Short: "Manage immutable gallery thumbnail generations",
	}
	workers := min(runtime.NumCPU(), 4)
	backfill := &cobra.Command{
		Use:          "backfill",
		Short:        "Generate missing progressive Ultra HDR gallery variants",
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			stats, err := apis.BackfillGalleryThumbs(ctx, app, workers)
			_, _ = fmt.Fprintf(command.OutOrStdout(),
				"Gallery thumbnail backfill: generated=%d skipped=%d failed=%d\n",
				stats.Generated, stats.Skipped, stats.Failed)
			return err
		},
	}
	backfill.Flags().IntVar(&workers, "workers", workers, "maximum concurrent photo generations")
	command.AddCommand(backfill)
	return command
}
