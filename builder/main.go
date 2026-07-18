package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		runBuildCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runBuildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	format := fs.String("format", "", "override [output] -> format from the spec file (rootfs | iso | qcow2)")
	out := fs.String("out", "", "override [output] -> path from the spec file")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: builder build <spec.hk> [--format rootfs|iso|qcow2] [--out DIR]")
		os.Exit(1)
	}

	spec, err := ParseBuildSpec(rest[0])
	if err != nil {
		logErr(err.Error())
		os.Exit(1)
	}
	if *format != "" {
		spec.OutputFormat = *format
	}
	if *out != "" {
		spec.OutputPath = *out
	}

	if err := RunBuild(spec); err != nil {
		logErr(err.Error())
		os.Exit(1)
	}
}

const usageText = `builder — Isolator's distro/image builder (separate tool, requires isolator on PATH)

Usage:
  builder build <spec.hk> [--format rootfs|iso|qcow2] [--out DIR]
  builder help

A spec file describes a minimal base image (kernel + podman + isolator —
everything else is meant to be installed afterward as Isolator-managed
containers):

  [distro]
  -> name   => hackeros-mini
  -> base   => debian
  -> suite  => bookworm

  [kernel]
  -> package => linux-image-amd64

  [packages]
  -> curl
  -> vim

  [output]
  -> format => rootfs
  -> path   => ./build-output

Live-build-inspired companion directory (optional, same base name as the
spec file, e.g. "hackeros-mini/" next to "hackeros-mini.hk"):

  hackeros-mini/
    package-lists/*.list.chroot   one package name per line
    hooks/*.hook.chroot           shell scripts run inside the chroot,
                                   in sorted filename order
    includes.chroot/              files copied verbatim into the rootfs`

func printUsage() {
	fmt.Println(usageText)
}
