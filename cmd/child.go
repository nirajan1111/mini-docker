package cmd

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/runtime"
	"github.com/spf13/cobra"
)

// childCmd is an internal command invoked by the parent after namespace creation.
// It runs inside the new namespaces and sets up the container environment.
var childCmd = &cobra.Command{
	Use:    "_child",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		containerID, _ := cmd.Flags().GetString("id")
		imageName, _ := cmd.Flags().GetString("image")
		imageTag, _ := cmd.Flags().GetString("tag")
		command, _ := cmd.Flags().GetString("cmd")
		cmdArgs, _ := cmd.Flags().GetString("args")
		memLimit, _ := cmd.Flags().GetInt("mem")
		pidLimit, _ := cmd.Flags().GetInt("pids")
		cpuLimit, _ := cmd.Flags().GetInt("cpus")
		containerName, _ := cmd.Flags().GetString("name")

		var parsedArgs []string
		if cmdArgs != "" {
			parsedArgs = strings.Split(cmdArgs, "\x00")
		}

		rt := runtime.New()
		exitCode := rt.InitContainer(runtime.InitRequest{
			ContainerID:   containerID,
			ContainerName: containerName,
			ImageName:     imageName,
			ImageTag:      imageTag,
			Command:       command,
			Args:          parsedArgs,
			MemoryLimit:   memLimit,
			PidLimit:      pidLimit,
			CPULimit:      cpuLimit,
		})

		log.Debugf("container exited with code %d", exitCode)
		os.Exit(exitCode)
		return nil
	},
}

func init() {
	childCmd.Flags().String("id", "", "container ID")
	childCmd.Flags().String("image", "", "image name")
	childCmd.Flags().String("tag", "", "image tag")
	childCmd.Flags().String("cmd", "", "command to run")
	childCmd.Flags().String("args", "", "null-separated command arguments")
	childCmd.Flags().String("name", "", "container name")
	childCmd.Flags().Int("mem", 0, "memory limit")
	childCmd.Flags().Int("pids", 0, "pid limit")
	childCmd.Flags().Int("cpus", 0, "cpu limit")

	rootCmd.AddCommand(childCmd)
}
