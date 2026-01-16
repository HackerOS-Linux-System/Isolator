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

	"github.com/BurntSushi/toml"
	"github.com/otiai10/copy"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	rootfsBaseDir   = "/var/lib/isolator/rootfs"
	defaultImage    = "chainguard/wolfi-base" // Wolfi integration
	configFileName  = ".isolator.toml"
	hackerFileName  = ".hacker"
	hackerStart     = "["
	hackerEnd       = "]"
)

type Config struct {
	DefaultRootfs string `toml:"default_rootfs"`
	AutoGPU       bool   `toml:"auto_gpu"`
	AutoGUI       bool   `toml:"auto_gui"`
	CustomCommands map[string]CustomCommand `toml:"custom_commands"`
}

type CustomCommand struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	GPU     bool     `toml:"gpu"`
	GUI     bool     `toml:"gui"`
}

type HackerFile struct {
	From     string            `yaml:"from"`
	Commands []string          `yaml:"commands"`
	Env      map[string]string `yaml:"env"`
	Ports    []string          `yaml:"ports"`
	Volumes  []string          `yaml:"volumes"`
}

var (
	globalConfig Config
	gpuFlag      bool
	guiFlag      bool
)

func main() {
	// Load global config if exists
	loadConfig()

	// Use pterm for styled header
	pterm.DefaultHeader.WithBackgroundStyle(pterm.NewStyle(pterm.BgCyan)).WithTextStyle(pterm.NewStyle(pterm.FgBlack)).Println("Isolator - Lightweight Container Tool")

	rootCmd := &cobra.Command{
		Use:   "isolator",
		Short: "A lightweight container tool similar to Podman but with less isolation for better performance",
		Long: `Isolator is a custom container runtime that uses namespaces for lightweight isolation.
It supports GPU and GUI applications out of the box. Defaults to Wolfi base images for lightness.
Supports .toml config for customization and .hacker files for build-like definitions.
		
WARNING: GPU support provides full host GPU access - use with caution!`,
	}

	pullCmd := &cobra.Command{
		Use:   "pull [image]",
		Short: "Pull and extract rootfs from Docker/Podman image",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			image := args[0]
			if image == "" {
				image = defaultImage
			}
			pullImage(image)
		},
	}

	buildCmd := &cobra.Command{
		Use:   "build [path_to_hackerfile_dir]",
		Short: "Build rootfs from .hacker file (like Dockerfile but in custom format)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			dir := args[0]
			buildFromHackerFile(dir)
		},
	}

	runCmd := &cobra.Command{
		Use:   "run [rootfs_name] [command] [args...]",
		Short: "Run command in container",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			rootfsName := args[0]
			if rootfsName == "" && globalConfig.DefaultRootfs != "" {
				rootfsName = globalConfig.DefaultRootfs
			}
			cmdArgs := args[1:]
			// Override flags with global config if not set
			if !cmd.Flags().Changed("gpu") {
				gpuFlag = globalConfig.AutoGPU
			}
			if !cmd.Flags().Changed("gui") {
				guiFlag = globalConfig.AutoGUI
			}
			if gpuFlag {
				pterm.Warning.Println("GPU enabled: Full host GPU access granted - potential security risks!")
			}
			runContainer(rootfsName, cmdArgs)
		},
	}
	runCmd.Flags().BoolVar(&gpuFlag, "gpu", false, "Enable GPU support")
	runCmd.Flags().BoolVar(&guiFlag, "gui", false, "Enable GUI support")

	execCmd := &cobra.Command{
		Use:   "exec [custom_command_name]",
		Short: "Execute a custom command defined in .toml config",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if cc, ok := globalConfig.CustomCommands[name]; ok {
				rootfsName := globalConfig.DefaultRootfs
				if rootfsName == "" {
					pterm.Error.Println("No default rootfs set in config.")
					os.Exit(1)
				}
				gpuFlag = cc.GPU
				guiFlag = cc.GUI
				runContainer(rootfsName, append([]string{cc.Command}, cc.Args...))
			} else {
				pterm.Error.Printf("Custom command %s not found.\n", name)
				os.Exit(1)
			}
		},
	}

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

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show current configuration",
		Run: func(cmd *cobra.Command, args []string) {
			showConfig()
		},
	}

	rootCmd.AddCommand(pullCmd, buildCmd, runCmd, execCmd, listCmd, rmCmd, configCmd)
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
}

func loadConfig() {
	if _, err := os.Stat(configFileName); err == nil {
		if _, err := toml.DecodeFile(configFileName, &globalConfig); err != nil {
			pterm.Warning.Printf("Error loading config: %v\n", err)
		} else {
			pterm.Success.Println("Loaded configuration from .isolator.toml")
		}
	}
}

func showConfig() {
	pterm.Info.Println("Current Configuration:")
	pterm.NewStyle(pterm.FgCyan).Printf("Default Rootfs: %s\n", globalConfig.DefaultRootfs)
	pterm.NewStyle(pterm.FgCyan).Printf("Auto GPU: %t\n", globalConfig.AutoGPU)
	pterm.NewStyle(pterm.FgCyan).Printf("Auto GUI: %t\n", globalConfig.AutoGUI)
	if len(globalConfig.CustomCommands) > 0 {
		pterm.Info.Println("Custom Commands:")
		for name, cc := range globalConfig.CustomCommands {
			pterm.NewStyle(pterm.FgGreen).Printf("- %s: %s %v (GPU: %t, GUI: %t)\n", name, cc.Command, cc.Args, cc.GPU, cc.GUI)
		}
	}
}

func buildFromHackerFile(dir string) {
	hackerPath := filepath.Join(dir, hackerFileName)
	data, err := os.ReadFile(hackerPath)
	if err != nil {
		pterm.Error.Printf("Error reading .hacker file: %v\n", err)
		os.Exit(1)
	}

	content := string(data)
	if !strings.HasPrefix(content, hackerStart) || !strings.HasSuffix(content, hackerEnd) {
		pterm.Error.Println(".hacker file must start with [ and end with ]")
		os.Exit(1)
	}

	// Extract inner content and parse as YAML
	inner := strings.TrimSpace(content[len(hackerStart):len(content)-len(hackerEnd)])
	var hf HackerFile
	if err := yaml.Unmarshal([]byte(inner), &hf); err != nil {
		pterm.Error.Printf("Error parsing YAML in .hacker: %v\n", err)
		os.Exit(1)
	}

	// Pull base image
	pullImage(hf.From)

	// Copy base rootfs for building to avoid mutating original
	baseRootfsName := sanitizeName(hf.From)
	baseRootfsDir := filepath.Join(rootfsBaseDir, baseRootfsName)
	rootfsName := baseRootfsName + "-built"
	rootfsDir := filepath.Join(rootfsBaseDir, rootfsName)

	pterm.Info.Printf("Copying base rootfs for build...\n")
	copySpinner, _ := pterm.DefaultSpinner.Start("Copying rootfs...")
	if err := copy.Copy(baseRootfsDir, rootfsDir); err != nil {
		copySpinner.Fail("Error copying rootfs")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	copySpinner.Success("Rootfs copied")

	// Run build commands in container
	for _, cmdStr := range hf.Commands {
		cmdParts := strings.Fields(cmdStr)
		if len(cmdParts) == 0 {
			continue
		}
		pterm.Info.Printf("Running build command: %s\n", cmdStr)
		runContainer(rootfsName, cmdParts)
	}

	// Apply env, ports, volumes (log for now; expandable)
	box := pterm.DefaultBox.WithTitle("Hackerfile Config")
	boxContent := fmt.Sprintf("Environment: %v\nPorts: %v\nVolumes: %v", hf.Env, hf.Ports, hf.Volumes)
	pterm.Println(box.Sprint(boxContent))

	pterm.Success.Printf("Build complete for %s\n", rootfsName)
}

func pullImage(image string) {
	rootfsDir := filepath.Join(rootfsBaseDir, sanitizeName(image))

	if err := os.MkdirAll(rootfsBaseDir, 0755); err != nil {
		pterm.Error.Printf("Error creating base dir: %v\n", err)
		os.Exit(1)
	}

	pterm.Info.Printf("Pulling image %s...\n", image)
	pullSpinner, _ := pterm.DefaultSpinner.WithStyle(pterm.NewStyle(pterm.FgCyan)).Start("Pulling image...")
	pullCmd := exec.Command("podman", "pull", image)
	if err := pullCmd.Run(); err != nil {
		pullSpinner.Fail("Error pulling image")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	pullSpinner.Success("Image pulled")

	tempContainer := "isolator-temp-" + sanitizeName(image)
	createSpinner, _ := pterm.DefaultSpinner.WithStyle(pterm.NewStyle(pterm.FgCyan)).Start("Creating temp container...")
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

	tarFile := filepath.Join(rootfsBaseDir, sanitizeName(image)+".tar")
	exportSpinner, _ := pterm.DefaultSpinner.WithStyle(pterm.NewStyle(pterm.FgCyan)).Start("Exporting container...")
	exportCmd := exec.Command("podman", "export", tempContainer, "-o", tarFile)
	if err := exportCmd.Run(); err != nil {
		exportSpinner.Fail("Error exporting container")
		pterm.Error.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	exportSpinner.Success("Container exported")
	defer os.Remove(tarFile)

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

	fi, _ := f.Stat()
	totalSize := fi.Size()

	progressBar, _ := pterm.DefaultProgressbar.WithTotal(int(totalSize)).WithTitle("Extracting rootfs").WithBarStyle(pterm.NewStyle(pterm.FgGreen)).Start()
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
		}
	}
	progressBar.Stop()
	pterm.Success.Println("Pull complete.")
}

func runContainer(rootfsName string, cmdArgs []string) {
	rootfsDir := filepath.Join(rootfsBaseDir, rootfsName)
	if _, err := os.Stat(rootfsDir); os.IsNotExist(err) {
		pterm.Error.Printf("Rootfs %s not found. Pull or build it first.\n", rootfsName)
		os.Exit(1)
	}

	childArgs := []string{"child", rootfsDir}
	if gpuFlag {
		childArgs = append(childArgs, "--gpu")
	}
	if guiFlag {
		childArgs = append(childArgs, "--gui")
	}
	childArgs = append(childArgs, cmdArgs...)

	pterm.Info.Printf("Starting container %s...\n", rootfsName)
	parentCmd := exec.Command("/proc/self/exe", childArgs...)
	parentCmd.Stdin = os.Stdin
	parentCmd.Stdout = os.Stdout
	parentCmd.Stderr = os.Stderr
	parentCmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER | syscall.CLONE_NEWIPC | syscall.CLONE_NEWNET,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: 0, Size: 65536}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: 0, Size: 65536}},
		GidMappingsEnableSetgroups: false,
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

	// Make root private to prevent mount propagation
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))

	must(syscall.Mount(rootfsDir, rootfsDir, "", syscall.MS_BIND, ""))
	must(os.MkdirAll(filepath.Join(rootfsDir, "oldrootfs"), 0700))
	must(syscall.PivotRoot(rootfsDir, filepath.Join(rootfsDir, "oldrootfs")))
	must(os.Chdir("/"))

	// Cleanup oldrootfs
	must(syscall.Unmount("/oldrootfs", syscall.MNT_DETACH))
	os.Remove("/oldrootfs")

	must(syscall.Mount("proc", "/proc", "proc", 0, ""))
	must(syscall.Mount("sysfs", "/sys", "sysfs", 0, ""))
	must(syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755"))
	must(syscall.Mount("devpts", "/dev/pts", "devpts", 0, ""))
	must(syscall.Mount("tmpfs", "/run", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV|syscall.MS_STRICTATIME, "mode=755"))

	// Setup network loopback
	pterm.Info.Println("Setting up loopback interface...")
	netCmd := exec.Command("ip", "link", "set", "lo", "up")
	if err := netCmd.Run(); err != nil {
		pterm.Warning.Printf("Failed to setup loopback: %v\n", err)
	}

	if gpu {
		pterm.Info.Println("Enabling GPU support...")
		devices := []string{"/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/nvidia0", "/dev/nvidia1", "/dev/dri"}
		for _, dev := range devices {
			if _, err := os.Stat(dev); err == nil {
				must(syscall.Mount(dev, dev, "bind", syscall.MS_BIND|syscall.MS_REC, ""))
			}
		}
	}

	var env []string
	if gui {
		pterm.Info.Println("Enabling GUI support...")
		display := os.Getenv("DISPLAY")
		if display == "" {
			display = ":0"
		}
		env = append(os.Environ(), "DISPLAY="+display)
		must(syscall.Mount("/tmp/.X11-unix", "/tmp/.X11-unix", "bind", syscall.MS_BIND|syscall.MS_REC, ""))
	} else {
		env = os.Environ()
	}

	startSpinner, _ := pterm.DefaultSpinner.WithStyle(pterm.NewStyle(pterm.FgMagenta)).Start("Starting command...")
	time.Sleep(1 * time.Second)
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
	listItems := []pterm.BulletListItem{}
	for _, file := range files {
		if file.IsDir() {
			listItems = append(listItems, pterm.BulletListItem{Level: 0, Text: file.Name()})
		}
	}
	pterm.DefaultBulletList.WithItems(listItems).WithBulletStyle(pterm.NewStyle(pterm.FgYellow)).Render()
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
