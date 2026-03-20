package main

import (
	"isolator/src"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	// Check if podman is available
	if err := src.CheckPodman(); err != nil {
		src.PrintError(err.Error())
		os.Exit(1)
	}

	var rootCmd = &cobra.Command{
		Use:           "isolator",
		Short:         "Podman-based package manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			src.PrintColoredHelp()
		},
	}

	installCmd := &cobra.Command{
		Use:   "install <pkg>",
		Short: "Install a package",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			isolated, _ := cmd.Flags().GetBool("isolated")
			src.HandleInstall(args[0], isolated)
		},
	}
	installCmd.Flags().Bool("isolated", false, "Install in isolated container with its own home directory")

	rootCmd.AddCommand(
		installCmd,
		&cobra.Command{
			Use:   "remove <pkg>",
			Short: "Remove an installed package",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   src.HandleRemove(args[0])
			   },
		},
		&cobra.Command{
			Use:   "search <term>",
			Short: "Search for a package",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   src.HandleSearch(args[0])
			   },
		},
		&cobra.Command{
			Use:   "info <pkg>",
			Short: "Show package information",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   src.HandleInfo(args[0])
			   },
		},
		&cobra.Command{
			Use:   "list",
			Short: "List installed packages",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleList()
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show container status dashboard",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleStatus()
			},
		},
		&cobra.Command{
			Use:   "update",
			Short: "Update all containers",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleUpdate()
			},
		},
		&cobra.Command{
			Use:   "refresh",
			Short: "Force re-download of the repository list",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleRefresh()
			},
		},
		&cobra.Command{
			Use:   "upgrade",
			Short: "Full system upgrade (host + containers)",
			   Args:  cobra.NoArgs,
			   Run: func(cmd *cobra.Command, args []string) {
				   src.HandleUpgrade()
			   },
		},
	)

	if err := rootCmd.Execute(); err != nil {
		src.PrintError(err.Error())
		os.Exit(1)
	}
}
