package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nirajansah/mini-docker/storage"
	"github.com/spf13/cobra"
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List downloaded images",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := storage.NewImageStore()
		images, err := store.List()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NAME\tTAG\tLAYERS\tCREATED")
		for _, img := range images {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
				img.Name, img.Tag, img.LayerCount,
				img.CreatedAt.Format("2006-01-02 15:04:05"),
			)
		}
		w.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(imagesCmd)
}
