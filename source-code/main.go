package main

import (
	"fmt"
	"isolator/src"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	// --version, -h/--help, and bare `help` shouldn't require podman to be
	// installed — someone checking "what version is this" or reading the
	// help text is often doing exactly that because podman ISN'T set up
	// yet.
	skipPodmanCheck := false
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--version", "-v", "version", "--help", "-h", "help", "docs":
			skipPodmanCheck = true
		}
	}
	for _, a := range os.Args[1:] {
		if a == "--help" || a == "-h" {
			skipPodmanCheck = true
		}
	}
	if !skipPodmanCheck {
		if err := src.CheckPodman(); err != nil {
			src.PrintError(err.Error())
			os.Exit(1)
		}
	}

	// `isolator myproject.hk` — declarative dev environment activation,
	// handled before cobra's subcommand dispatch since it's not a
	// subcommand, it's a positional file argument.
	if path, ok := src.IsEnvFile(os.Args[1:]); ok {
		src.HandleEnvFile(path)
		return
	}

	var rootCmd = &cobra.Command{
		Use:           "isolator",
		Short:         "Podman-based package manager",
		Version:       src.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			src.PrintColoredHelp()
		},
	}
	rootCmd.SetVersionTemplate("isolator {{.Version}}\n")

	installCmd := &cobra.Command{
		Use:   "install <pkg>",
		Short: "Install a package",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			isolated, _ := cmd.Flags().GetBool("isolated")
			if !cmd.Flags().Changed("isolated") {
				isolated = src.LoadConfig().DefaultIsolated
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleInstall(args[0], isolated, dryRun)
		},
	}
	installCmd.Flags().Bool("isolated", false, "Install in isolated container with its own home directory")
	installCmd.Flags().Bool("dry-run", false, "Show what would happen without installing anything")

	removeCmd := &cobra.Command{
		Use:   "remove <pkg>",
		Short: "Remove an installed package",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			force, _ := cmd.Flags().GetBool("force")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleRemove(args[0], force, dryRun)
		},
	}
	removeCmd.Flags().Bool("force", false, "Remove even if other installed packages depend on it")
	removeCmd.Flags().Bool("dry-run", false, "Show what would happen without removing anything")

	execCmd := &cobra.Command{
		Use:   "exec <pkg> -- <command> [args...]",
		Short: "Run an arbitrary command inside a package's container",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			src.HandleExec(args[0], args[1:])
		},
	}

	snapshotCmd := &cobra.Command{
		Use:   "snapshot [container]",
		Short: "Save a rollback point for a container (or every managed container with --all)",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			all, _ := cmd.Flags().GetBool("all")
			if all {
				src.HandleSnapshotAll(dryRun)
				return
			}
			if len(args) != 1 {
				fmt.Fprintln(os.Stderr, "usage: isolator snapshot <container> | isolator snapshot --all")
				os.Exit(1)
			}
			src.HandleSnapshot(args[0], dryRun)
		},
	}
	snapshotCmd.Flags().Bool("dry-run", false, "Show what would be snapshotted without doing it")
	snapshotCmd.Flags().Bool("all", false, "Snapshot every managed container")

	rollbackCmd := &cobra.Command{
		Use:   "rollback [container]",
		Short: "Restore a container (or, with --all, every managed container) from its most recent snapshot",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			all, _ := cmd.Flags().GetBool("all")
			if all {
				src.HandleRollbackAll(dryRun)
				return
			}
			if len(args) != 1 {
				fmt.Fprintln(os.Stderr, "usage: isolator rollback <container> | isolator rollback --all")
				os.Exit(1)
			}
			src.HandleRollback(args[0], dryRun)
		},
	}
	rollbackCmd.Flags().Bool("dry-run", false, "Show what would be rolled back without doing it")
	rollbackCmd.Flags().Bool("all", false, "Roll back every managed container that has a snapshot — a real system-wide rollback")

	snapshotsCmd := &cobra.Command{
		Use:   "snapshots",
		Short: "List saved snapshots",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			src.HandleSnapshotList()
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update all containers",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleUpdate(dryRun)
		},
	}
	updateCmd.Flags().Bool("dry-run", false, "Show what would be updated without doing it")

	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Full system upgrade (host + containers)",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleUpgrade(dryRun)
		},
	}
	upgradeCmd.Flags().Bool("dry-run", false, "Show what would be upgraded without doing it")

	autoremoveCmd := &cobra.Command{
		Use:   "autoremove",
		Short: "Remove orphaned containers with no installed packages left",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleAutoremove(dryRun)
		},
	}
	autoremoveCmd.Flags().Bool("dry-run", false, "Show what would be removed without doing it")

	cleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Prune dangling Podman images and build cache",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			src.HandleClean(dryRun)
		},
	}
	cleanCmd.Flags().Bool("dry-run", false, "Show what would be cleaned without doing it")

	rootCmd.AddCommand(
		installCmd,
		removeCmd,
		execCmd,
		snapshotCmd,
		rollbackCmd,
		snapshotsCmd,
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
		updateCmd,
		&cobra.Command{
			Use:   "refresh",
			Short: "Force re-download of the repository list",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleRefresh()
			},
		},
		upgradeCmd,
		autoremoveCmd,
		cleanCmd,
		&cobra.Command{
			Use:   "version",
			Short: "Print the isolator version",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Println("isolator " + src.Version)
			},
		},
		&cobra.Command{
			Use:   "docs",
			Short: "Open the online Isolator documentation in your browser",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleDocs()
			},
		},
		&cobra.Command{
			Use:   "init",
			Short: "First-run setup: config, PATH check, GPU/audio detection",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				src.HandleInit()
			},
		},
	)

	if err := rootCmd.Execute(); err != nil {
		src.PrintError(err.Error())
		os.Exit(1)
	}
}
