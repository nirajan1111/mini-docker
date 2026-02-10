package cmd

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var logLevel string

var rootCmd = &cobra.Command{
	Use:   "mini-docker",
	Short: "A minimal container runtime built from scratch",
	Long:  "mini-docker is an educational container runtime implementing namespaces, cgroups, overlayfs, and networking.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		level, err := log.ParseLevel(logLevel)
		if err != nil {
			level = log.InfoLevel
		}
		log.SetLevel(level)
		log.SetFormatter(&log.TextFormatter{
			FullTimestamp: true,
		})

		// Commands that don't need root
		if cmd.Name() == "help" || cmd.Name() == "mini-docker" {
			return
		}

		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "mini-docker must be run as root")
			os.Exit(1)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (trace, debug, info, warn, error)")
}
