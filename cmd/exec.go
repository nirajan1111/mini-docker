package cmd

import (
	"fmt"

	"github.com/nirajansah/mini-docker/runtime"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec CONTAINER_ID COMMAND [ARG...]",
	Short: "Execute a command in a running container",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		containerID := args[0]
		command := args[1]
		cmdArgs := args[2:]

		rt := runtime.New()
		if err := rt.Exec(containerID, command, cmdArgs); err != nil {
			return fmt.Errorf("exec failed: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
