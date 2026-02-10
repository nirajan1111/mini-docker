package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nirajansah/mini-docker/storage"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List containers",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := storage.NewContainerStore()
		containers, err := store.List()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tCOMMAND\tCREATED\tSTATUS\tNAME")
		for _, c := range containers {
			status := "Exited"
			if c.ExitCode == nil {
				status = "Running"
			} else {
				status = fmt.Sprintf("Exited (%d)", *c.ExitCode)
			}
			fmt.Fprintf(w, "%s\t%s:%s\t%s\t%s\t%s\t%s\n",
				c.ID[:12],
				c.ImageName, c.ImageTag,
				c.Command,
				c.CreatedAt.Format("2006-01-02 15:04:05"),
				status,
				c.Name,
			)
		}
		w.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
