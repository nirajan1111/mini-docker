package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/nirajansah/mini-docker/config"
	"github.com/nirajansah/mini-docker/runtime"
	"github.com/spf13/cobra"
)

var (
	containerName string
	memoryLimit   int
	pidLimit      int
	cpuLimit      int
)

var runCmd = &cobra.Command{
	Use:   "run [OPTIONS] IMAGE [COMMAND] [ARG...]",
	Short: "Run a command in a new container",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		imgRef := args[0]
		name, tag := parseImageRef(imgRef)

		var command string
		var cmdArgs []string
		if len(args) > 1 {
			command = args[1]
			cmdArgs = args[2:]
		} else {
			command = "/bin/sh"
		}

		rt := runtime.New()
		exitCode, err := rt.Run(runtime.RunRequest{
			ImageName:     name,
			ImageTag:      tag,
			Command:       command,
			Args:          cmdArgs,
			ContainerName: containerName,
			MemoryLimit:   memoryLimit,
			PidLimit:      pidLimit,
			CPULimit:      cpuLimit,
		})
		if err != nil {
			return fmt.Errorf("container run failed: %w", err)
		}
		os.Exit(exitCode)
		return nil
	},
}

func parseImageRef(ref string) (string, string) {
	parts := strings.SplitN(ref, ":", 2)
	name := parts[0]
	tag := config.DefaultTag
	if len(parts) == 2 {
		tag = parts[1]
	}
	return name, tag
}

func init() {
	runCmd.Flags().StringVar(&containerName, "name", "", "assign a name to the container")
	runCmd.Flags().IntVarP(&memoryLimit, "memory", "m", config.DefaultMemLimit, "memory limit in bytes")
	runCmd.Flags().IntVar(&pidLimit, "pids-limit", config.DefaultPidLimit, "max number of processes")
	runCmd.Flags().IntVar(&cpuLimit, "cpus", config.DefaultCPULimit, "number of CPUs")
	rootCmd.AddCommand(runCmd)
}
