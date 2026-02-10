package cmd

import (
	"fmt"

	"github.com/nirajansah/mini-docker/storage"
	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm CONTAINER_ID",
	Short: "Remove a container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := storage.NewContainerStore()
		if err := store.Remove(args[0]); err != nil {
			return fmt.Errorf("remove failed: %w", err)
		}
		fmt.Printf("Removed container %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rmCmd)
}
