package cmd

import (
	"fmt"

	"github.com/nirajansah/mini-docker/image"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull IMAGE[:TAG]",
	Short: "Pull an image from Docker Hub",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, tag := parseImageRef(args[0])
		reg := image.NewRegistry()
		if err := reg.Pull(name, tag); err != nil {
			return fmt.Errorf("pull failed: %w", err)
		}
		fmt.Printf("Successfully pulled %s:%s\n", name, tag)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullCmd)
}
