package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

const (
	rootfsBaseDir = "/var/lib/isolator/rootfs"
	defaultImage  = "chainguard/wolfi-base" // Integrate Wolfi as default or optional
)

var (
	gpuFlag bool
	guiFlag bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "isolator",
		Short: "A lightweight container tool similar to Podman but with less isolation for better performance",
		Long: `Isolator is a custom container runtime that uses namespaces for lightweight isolation.
It supports GPU and GUI applications out of the box. Defaults to Wolfi base images for lightness.`,
	}

	pullCmd := &cobra.Command{
		Use:   "pull [image]",
		Short: "Pull and extract rootfs from Docker/Podman image",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			image := args[0]
			if image == "" {
				image = defaultImage // Use Wolfi if no image specified
			}
			pullImage(image)
		},
	}

	runCmd := &cobra.Command{
		Use:   "run [rootfs_name] [command] [args...]",
		Short: "Run command in container",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			rootfsName := args[0]
			cmdArgs := args[1:]
			runContainer(rootfsName, cmdArgs)
		},
	}
	runCmd.Flags().BoolVar(&gpuFlag, "gpu", false, "Enable GPU support")
	runCmd.Flags().BoolVar(&guiFlag, "gui", false, "Enable GUI support")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available rootfs",
		Run: func(cmd *cobra.Command, args []string) {
			listRootfs()
		},
	}

	rmCmd := &cobra.Command{
		Use:   "rm [rootfs_name]",
		Short: "Remove a rootfs",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			removeRootfs(args[0])
		},
	}

	rootCmd.AddCommand(pullCmd, runCmd, listCmd, rmCmd)
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
}

func pullImage(image string) {
	rootfsDir := filepath.Join(rootfsBaseDir, sanitizeName(image))

	// Create directories
	if err := os.MkdirAll(rootfsBaseDir, 0755); err != nil {
		pterm.Error.Printf("Error creating base dir: %v\n", err)
		os.Exit(1)
	}

	// Use podman to pull image with progress
	pterm.Info.Printf("Pulling image %s...\n", image)
	pullSpinner, _ := pterm.DefaultSpinner.Start("Pulling image...")
	pullCmd := exec.Command("podman", "pull", image)
	if err := pullCmd.Run(); err != nil {
		pullSpinner.Fail("Error pulling image")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	pullSpinner.Success("Image pulled")

	// Create a temporary container
	tempContainer := "isolator-temp-" + sanitizeName(image)
	createSpinner, _ := pterm.DefaultSpinner.Start("Creating temp container...")
	createCmd := exec.Command("podman", "create", "--name", tempContainer, image)
	if err := createCmd.Run(); err != nil {
		createSpinner.Fail("Error creating temp container")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	createSpinner.Success("Temp container created")
	defer func() {
		exec.Command("podman", "rm", "-f", tempContainer).Run()
	}()

	// Export to tar
	tarFile := filepath.Join(rootfsBaseDir, sanitizeName(image)+".tar")
	exportSpinner, _ := pterm.DefaultSpinner.Start("Exporting container...")
	exportCmd := exec.Command("podman", "export", tempContainer, "-o", tarFile)
	if err := exportCmd.Run(); err != nil {
		exportSpinner.Fail("Error exporting container")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	exportSpinner.Success("Container exported")
	defer os.Remove(tarFile)

	// Extract tar to rootfs dir with progress bar
	pterm.Info.Printf("Extracting to %s...\n", rootfsDir)
	if err := os.MkdirAll(rootfsDir, 0755); err != nil {
		pterm.Error.Printf("Error creating rootfs dir: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Open(tarFile)
	if err != nil {
		pterm.Error.Printf("Error opening tar: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// Get tar size for progress
	fi, _ := f.Stat()
	totalSize := fi.Size()

	progressBar, _ := pterm.DefaultProgressbar.WithTotal(int(totalSize)).WithTitle("Extracting rootfs").Start()
	tr := tar.NewReader(&progressReader{reader: f, bar: progressBar})

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			progressBar.Stop()
			pterm.Error.Printf("Error reading tar: %v\n", err)
			os.Exit(1)
		}
		target := filepath.Join(rootfsDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				progressBar.Stop()
				pterm.Error.Printf("Error creating dir: %v\n", err)
				os.Exit(1)
			}
		case tar.TypeReg:
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				progressBar.Stop()
				pterm.Error.Printf("Error creating file: %v\n", err)
				os.Exit(1)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				progressBar.Stop()
				pterm.Error.Printf("Error copying file: %v\n", err)
				os.Exit(1)
			}
			outFile.Close()
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				progressBar.Stop()
				pterm.Error.Printf("Error creating symlink: %v\n", err)
				os.Exit(1)
			}
		// Add more types if needed
		}
	}
	progressBar.Stop()
	pterm.Success.Println("Pull complete.")
}

func runContainer(rootfsName string, cmdArgs []string) {
	rootfsDir := filepath.Join(rootfsBaseDir, rootfsName)
	if _, err := os.Stat(rootfsDir); os.IsNotExist(err) {
		pterm.Error.Printf("Rootfs %s not found. Pull it first.\n", rootfsName)
		os.Exit(1)
	}

	// Prepare child args: rootfsDir, gpu, gui, cmd, args...
	childArgs := []string{"child", rootfsDir}
	if gpuFlag {
		childArgs = append(childArgs, "--gpu")
	}
	if guiFlag {
		childArgs = append(childArgs, "--gui")
	}
	childArgs = append(childArgs, cmdArgs...)

	// Run parent process
	pterm.Info.Printf("Starting container %s...\n", rootfsName)
	parentCmd := exec.Command("/proc/self/exe", childArgs...)
	parentCmd.Stdin = os.Stdin
	parentCmd.Stdout = os.Stdout
	parentCmd.Stderr = os.Stderr
	parentCmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER | syscall.CLONE_NEWIPC | syscall.CLONE_NEWNET,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
	}

	if err := parentCmd.Run(); err != nil {
		pterm.Error.Printf("Error running container: %v\n", err)
		os.Exit(1)
	}
	pterm.Success.Println("Container exited.")
}

func child(args []string) {
	if len(args) < 2 {
		panic("Invalid child args")
	}
	rootfsDir := args[0]
	var gpu, gui bool
	i := 1
	for ; i < len(args); i++ {
		if args[i] == "--gpu" {
			gpu = true
		} else if args[i] == "--gui" {
			gui = true
		} else {
			break
		}
	}
	cmd := args[i]
	cmdArgs := args[i+1:]

	// Mount rootfs
	must(syscall.Mount(rootfsDir, rootfsDir, "", syscall.MS_BIND, ""))
	must(os.MkdirAll(filepath.Join(rootfsDir, "oldrootfs"), 0700))
	must(syscall.PivotRoot(rootfsDir, filepath.Join(rootfsDir, "oldrootfs")))
	must(os.Chdir("/"))

	// Mount standard filesystems
	must(syscall.Mount("proc", "/proc", "proc", 0, ""))
	must(syscall.Mount("sysfs", "/sys", "sysfs", 0, ""))
	must(syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755"))
	must(syscall.Mount("devpts", "/dev/pts", "devpts", 0, ""))
	must(syscall.Mount("tmpfs", "/run", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_STRICTATIME, "mode=755"))

	// GPU support
	if gpu {
		pterm.Info.Println("Enabling GPU support...")
		devices := []string{"/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/nvidia0", "/dev/nvidia1"} // More devices
		for _, dev := range devices {
			if _, err := os.Stat(dev); err == nil {
				must(syscall.Mount(dev, dev, "bind", syscall.MS_BIND|syscall.MS_REC, ""))
			}
		}
		// Assume rootfs has necessary libs; for Wolfi, ensure image has them
	}

	// GUI support
	var env []string
	if gui {
		pterm.Info.Println("Enabling GUI support...")
		display := os.Getenv("DISPLAY")
		if display == "" {
			display = ":0"
		}
		env = append(os.Environ(), "DISPLAY="+display)
		must(syscall.Mount("/tmp/.X11-unix", "/tmp/.X11-unix", "bind", syscall.MS_BIND|syscall.MS_REC, ""))
		// Additional X auth if needed, but assume xhost +local: on host
	} else {
		env = os.Environ()
	}

	// Run command with spinner for startup
	startSpinner, _ := pterm.DefaultSpinner.Start("Starting command...")
	time.Sleep(1 * time.Second) // Simulate startup
	startSpinner.Success("Command started")

	childCmd := exec.Command(cmd, cmdArgs...)
	childCmd.Stdin = os.Stdin
	childCmd.Stdout = os.Stdout
	childCmd.Stderr = os.Stderr
	childCmd.Env = env

	if err := childCmd.Run(); err != nil {
		pterm.Error.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	syscall.Sync()
	os.Exit(0)
}

func listRootfs() {
	files, err := os.ReadDir(rootfsBaseDir)
	if err != nil {
		pterm.Error.Printf("Error listing rootfs: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		pterm.Info.Println("No rootfs available.")
		return
	}
	pterm.Info.Println("Available rootfs:")
	for _, file := range files {
		if file.IsDir() {
			pterm.BulletListPrinter{}.WithItems([]pterm.BulletListItem{{Level: 0, Text: file.Name()}}).Render()
		}
	}
}

func removeRootfs(name string) {
	dir := filepath.Join(rootfsBaseDir, name)
	if err := os.RemoveAll(dir); err != nil {
		pterm.Error.Printf("Error removing %s: %v\n", name, err)
		os.Exit(1)
	}
	pterm.Success.Printf("Removed rootfs %s\n", name)
}

func must(err error) {
	if err != nil {
		pterm.Error.Printf("Mount error: %v\n", err)
		panic(err)
	}
}

func sanitizeName(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), ":", "_")
}

type progressReader struct {
	reader io.Reader
	bar    *pterm.ProgressbarPrinter
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.bar.Add(n)
	return
}
