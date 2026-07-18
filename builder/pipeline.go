package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Preconditions
// ---------------------------------------------------------------------------

// CheckDependencies verifies the tools Builder itself needs. Builder is a
// separate binary from Isolator (its own module, its own source tree) but
// it's built to complement it, not replace it: it hard-requires an Isolator
// install to be present, both as a sanity signal that the target machine is
// actually set up for the Isolator ecosystem, and as the fallback bootstrap
// backend (see EnsureBootstrapTool) for hosts that don't have the target
// distro's native bootstrap tool installed.
func CheckDependencies() error {
	if _, err := exec.LookPath("isolator"); err != nil {
		return fmt.Errorf("isolator not found on PATH — Builder requires an Isolator install (https://github.com/HackerOS-Linux-System/Isolator)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		return fmt.Errorf("podman not found on PATH — required by both Isolator and Builder's container-backed bootstrap fallback")
	}
	return nil
}

func bootstrapToolFor(base string) string {
	switch base {
	case "debian", "ubuntu":
		return "debootstrap"
	case "archlinux":
		return "pacstrap"
	default:
		return ""
	}
}

// bootstrapHelperPackage is the Isolator catalog package that provides
// bootstrapToolFor(base) when it isn't natively on the host — used for the
// container fallback path.
func bootstrapHelperPackage(base string) string {
	switch base {
	case "debian", "ubuntu":
		return "debootstrap"
	case "archlinux":
		return "arch-install-scripts"
	default:
		return ""
	}
}

// EnsureBootstrapTool finds (or falls back to Isolator to obtain) the tool
// needed to bootstrap spec.Base. Two paths:
//
//  1. Fast path: the tool is already on the host's PATH — use it directly.
//     This is the well-tested path (validated against a real Ubuntu
//     `noble` bootstrap + chroot package install during development).
//  2. Fallback: the host doesn't have it (e.g. building a Debian-based
//     image from a Fedora host, or building an Arch-based image from
//     anywhere but Arch — pacstrap only works using the *host's* pacman).
//     Builder shells out to `isolator install <helper>` to get it into an
//     Isolator-managed container, and the bootstrap step runs inside that
//     container via `isolator exec`, with its output copied back out via
//     `podman cp`. Once copied out, the result is a completely ordinary
//     directory on the host — every step after bootstrapping (chroot
//     package install, hooks, includes, packaging) is identical regardless
//     of which path produced it.
func EnsureBootstrapTool(base string) (native bool, tool string, err error) {
	tool = bootstrapToolFor(base)
	if tool == "" {
		return false, "", fmt.Errorf("no bootstrap tool known for base %q", base)
	}
	if _, lookErr := exec.LookPath(tool); lookErr == nil {
		return true, tool, nil
	}

	helper := bootstrapHelperPackage(base)
	logStep(fmt.Sprintf("'%s' not found on this host — falling back to an Isolator container (installing '%s')", tool, helper))
	if !runVisible("isolator", "install", helper) {
		return false, "", fmt.Errorf("failed to install '%s' via isolator — install '%s' manually and retry, or run Builder on a host that already has it", helper, tool)
	}
	return false, helper, nil
}

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

func RunBuild(spec *BuildSpec) error {
	banner()

	if err := CheckDependencies(); err != nil {
		return err
	}

	native, containerPkg, err := EnsureBootstrapTool(spec.Base)
	if err != nil {
		return err
	}

	outAbs, err := filepath.Abs(spec.OutputPath)
	if err != nil {
		return err
	}
	rootfs := filepath.Join(outAbs, "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	logStep(fmt.Sprintf("Bootstrapping %s into %s (%s)", spec.Base, rootfs, boolLabel(native, "native", "via isolator container")))
	if err := runBootstrap(spec, containerPkg, native, rootfs); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	logDone("Base system bootstrapped")

	// From here on, the pipeline is identical whether the rootfs came from
	// the native path or was copied out of an Isolator container fallback
	// — it's just a directory now.
	if err := runPostBootstrap(spec, rootfs); err != nil {
		logWarn("Post-bootstrap steps reported errors: " + err.Error())
	}

	return packageOutput(spec, rootfs, outAbs)
}

// runPostBootstrap installs the kernel/packages, applies includes.chroot and
// hooks, embeds isolator, and writes hostname/first-boot config. It's the
// same regardless of which base was bootstrapped, except for how packages
// actually get installed (apt vs pacman), because pacstrap already installs
// packages as part of bootstrapping — arch's package step here only handles
// the [packages] + package-lists extras that weren't part of the initial
// pacstrap call.
func runPostBootstrap(spec *BuildSpec, rootfs string) error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if spec.Base != "archlinux" {
		logStep("Mounting /proc, /sys, /dev into the target rootfs")
		unmount, err := mountVirtualFS(rootfs)
		if err != nil {
			return fmt.Errorf("mounting virtual filesystems: %w", err)
		}
		defer unmount()

		if err := writeResolvConf(rootfs); err != nil {
			note(fmt.Errorf("writing resolv.conf: %w", err))
		}

		allPackages := append([]string{spec.KernelPackage, "podman", "ca-certificates"}, spec.Packages...)
		logStep(fmt.Sprintf("Installing %d package(s) into the chroot: %s", len(allPackages), strings.Join(allPackages, ", ")))
		if err := chrootAptInstall(spec, rootfs, allPackages); err != nil {
			// Kernel postinst hooks (update-grub, depmod, initramfs
			// generation) are notoriously finicky inside a bare chroot
			// with no real bootloader target — warn, don't hard-fail.
			logWarn("Package install reported errors (often just kernel postinst hooks like update-grub failing harmlessly in a chroot): " + err.Error())
		} else {
			logDone("Packages installed")
		}
	} else if len(spec.Packages) > 0 {
		logStep(fmt.Sprintf("Installing %d extra package(s) via arch-chroot: %s", len(spec.Packages), strings.Join(spec.Packages, ", ")))
		if err := archChrootInstall(rootfs, spec.Packages); err != nil {
			logWarn("Extra package install reported errors: " + err.Error())
		} else {
			logDone("Extra packages installed")
		}
	}

	if spec.IncludesDir != "" {
		logStep("Copying includes.chroot/ into the rootfs")
		if err := copyTree(spec.IncludesDir, rootfs); err != nil {
			logWarn("includes.chroot copy had errors: " + err.Error())
		} else {
			logDone("includes.chroot copied")
		}
	}

	if len(spec.Hooks) > 0 {
		logStep(fmt.Sprintf("Running %d hook(s)", len(spec.Hooks)))
		for _, h := range spec.Hooks {
			if err := runHook(rootfs, h); err != nil {
				logWarn(fmt.Sprintf("hook %s failed: %v", filepath.Base(h), err))
			} else {
				logDone("hook " + filepath.Base(h))
			}
		}
	}

	logStep("Embedding the isolator binary into the image")
	if err := embedIsolator(rootfs); err != nil {
		logWarn("Could not embed isolator binary: " + err.Error())
	} else {
		logDone("isolator binary embedded at /usr/local/bin/isolator")
	}

	if err := writeHostname(rootfs, spec.Hostname); err != nil {
		logWarn("Could not write /etc/hostname: " + err.Error())
	}
	if err := writeFirstBootUnit(rootfs); err != nil {
		logWarn("Could not write first-boot systemd unit: " + err.Error())
	}

	return firstErr
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

func runBootstrap(spec *BuildSpec, containerPkg string, native bool, rootfs string) error {
	if spec.Base == "archlinux" {
		return runPacstrapBootstrap(spec, containerPkg, native, rootfs)
	}
	return runDebootstrapBootstrap(spec, containerPkg, native, rootfs)
}

func runDebootstrapBootstrap(spec *BuildSpec, containerPkg string, native bool, rootfs string) error {
	if native {
		args := []string{"--variant=minbase", "--arch=amd64", spec.Suite, rootfs, spec.Mirror}
		return runVisibleErr("debootstrap", args...)
	}
	// Fallback: run inside the Isolator container that
	// `isolator install debootstrap` just set up, writing into that
	// container's own filesystem (chroot/mount privileges live there),
	// then copy the result out onto the host.
	cmd := fmt.Sprintf("debootstrap --variant=minbase --arch=amd64 %s /tmp/builder-rootfs %s", spec.Suite, spec.Mirror)
	if !runVisible("isolator", "exec", containerPkg, "--", "sh", "-c", cmd) {
		return fmt.Errorf("debootstrap failed inside the isolator container")
	}
	if !runVisible("podman", "cp", containerPkg+":/tmp/builder-rootfs/.", rootfs) {
		return fmt.Errorf("failed to copy bootstrapped rootfs out of the container")
	}
	return nil
}

func runPacstrapBootstrap(spec *BuildSpec, containerPkg string, native bool, rootfs string) error {
	basePackages := append([]string{"base", "linux", "podman"}, spec.Packages...)

	if native {
		runVisibleErr("pacman-key", "--init")
		runVisibleErr("pacman-key", "--populate", "archlinux")
		args := append([]string{"-c", "-G", rootfs}, basePackages...)
		return runVisibleErr("pacstrap", args...)
	}

	// Fallback: pacstrap fundamentally needs to run against a real
	// pacman + keyring, which only an Arch(-based) host or an Isolator
	// archlinux container actually has. Run it there instead, targeting a
	// path inside the container, then copy the result out — same pattern
	// as the debootstrap fallback.
	initCmd := "pacman-key --init && pacman-key --populate archlinux"
	if !runVisible("isolator", "exec", containerPkg, "--", "sh", "-c", initCmd) {
		logWarn("pacman-key init inside the container reported an issue (continuing — it may already be initialized)")
	}
	pacstrapCmd := fmt.Sprintf("pacstrap -c -G /tmp/builder-rootfs %s", strings.Join(basePackages, " "))
	if !runVisible("isolator", "exec", containerPkg, "--", "sh", "-c", pacstrapCmd) {
		return fmt.Errorf("pacstrap failed inside the isolator container")
	}
	if !runVisible("podman", "cp", containerPkg+":/tmp/builder-rootfs/.", rootfs) {
		return fmt.Errorf("failed to copy bootstrapped rootfs out of the container")
	}
	return nil
}

// ---------------------------------------------------------------------------
// chroot helpers
// ---------------------------------------------------------------------------

func mountVirtualFS(rootfs string) (unmount func(), err error) {
	mounts := []string{"proc", "sys", "dev"}
	mounted := []string{}
	for _, m := range mounts {
		target := filepath.Join(rootfs, m)
		if err := os.MkdirAll(target, 0755); err != nil {
			return nil, err
		}
		if !runVisible("mount", "--bind", "/"+m, target) {
			for i := len(mounted) - 1; i >= 0; i-- {
				runVisible("umount", filepath.Join(rootfs, mounted[i]))
			}
			return nil, fmt.Errorf("failed to bind-mount /%s", m)
		}
		mounted = append(mounted, m)
	}
	unmounted := false
	return func() {
		if unmounted {
			return
		}
		unmounted = true
		for i := len(mounted) - 1; i >= 0; i-- {
			runVisible("umount", filepath.Join(rootfs, mounted[i]))
		}
	}, nil
}

func writeResolvConf(rootfs string) error {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rootfs, "etc/resolv.conf"), data, 0644)
}

func chrootAptInstall(spec *BuildSpec, rootfs string, packages []string) error {
	sourcesLine := fmt.Sprintf("deb %s %s main universe\n", spec.Mirror, spec.Suite)
	if spec.Base == "debian" {
		sourcesLine = fmt.Sprintf("deb %s %s main\n", spec.Mirror, spec.Suite)
	}
	if err := os.WriteFile(filepath.Join(rootfs, "etc/apt/sources.list"), []byte(sourcesLine), 0644); err != nil {
		return err
	}

	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	updateCmd := exec.Command("chroot", rootfs, "/bin/sh", "-c", "apt-get update -qq")
	updateCmd.Env = env
	updateCmd.Stdout, updateCmd.Stderr = os.Stdout, os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}

	installCmd := exec.Command("chroot", rootfs, "/bin/sh", "-c",
		"apt-get install -y --no-install-recommends "+strings.Join(packages, " "))
	installCmd.Env = env
	installCmd.Stdout, installCmd.Stderr = os.Stdout, os.Stderr
	return installCmd.Run()
}

// archChrootInstall installs extra packages into an already-pacstrapped
// rootfs using arch-chroot (from arch-install-scripts), which handles the
// /proc,/sys,/dev binds itself — unlike our manual mountVirtualFS approach
// used for the apt-based pipeline.
func archChrootInstall(rootfs string, packages []string) error {
	tool := "arch-chroot"
	if _, err := exec.LookPath(tool); err != nil {
		// Fall back to a plain chroot with our own virtual-fs mounts if
		// arch-chroot itself isn't available on this host.
		unmount, err := mountVirtualFS(rootfs)
		if err != nil {
			return err
		}
		defer unmount()
		return runVisibleErr("chroot", rootfs, "/bin/sh", "-c",
			"pacman -S --noconfirm "+strings.Join(packages, " "))
	}
	return runVisibleErr(tool, rootfs, "pacman", "-S", "--noconfirm", strings.Join(packages, " "))
}

func runHook(rootfs, hookPath string) error {
	dest := filepath.Join(rootfs, "tmp", filepath.Base(hookPath))
	data, err := os.ReadFile(hookPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, 0755); err != nil {
		return err
	}
	defer os.Remove(dest)

	cmd := exec.Command("chroot", rootfs, "/bin/sh", "-c", "/tmp/"+filepath.Base(hookPath))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func embedIsolator(rootfs string) error {
	src, err := exec.LookPath("isolator")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	dest := filepath.Join(rootfs, "usr/local/bin/isolator")
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0755)
}

func writeHostname(rootfs, hostname string) error {
	etcDir := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(etcDir, "hostname"), []byte(hostname+"\n"), 0644)
}

// writeFirstBootUnit drops a systemd unit that runs `isolator init` the
// first time the built image actually boots, so it arrives with Isolator
// already set up instead of requiring a manual first step.
func writeFirstBootUnit(rootfs string) error {
	unit := `[Unit]
Description=Isolator first-boot setup
ConditionPathExists=!/etc/isolator-first-boot-done
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/isolator init
ExecStartPost=/usr/bin/touch /etc/isolator-first-boot-done
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	unitDir := filepath.Join(rootfs, "etc/systemd/system")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, "isolator-first-boot.service"), []byte(unit), 0644); err != nil {
		return err
	}
	wantsDir := filepath.Join(unitDir, "multi-user.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	link := filepath.Join(wantsDir, "isolator-first-boot.service")
	os.Remove(link)
	return os.Symlink("../isolator-first-boot.service", link)
}

// ---------------------------------------------------------------------------
// Packaging
// ---------------------------------------------------------------------------

func packageOutput(spec *BuildSpec, rootfs, outAbs string) error {
	switch spec.OutputFormat {
	case "rootfs":
		return packageRootfsFallback(spec, rootfs, outAbs)
	case "iso":
		return packageISO(spec, rootfs, outAbs)
	case "qcow2":
		return packageQcow2(spec, rootfs, outAbs)
	}
	return fmt.Errorf("unknown output format %q", spec.OutputFormat)
}

func packageISO(spec *BuildSpec, rootfs, outAbs string) error {
	logWarn("ISO output is experimental and needs grub-mkrescue + xorriso on the host.")
	if _, err := exec.LookPath("grub-mkrescue"); err != nil {
		logWarn("grub-mkrescue not found — falling back to a rootfs tarball only.")
		return packageRootfsFallback(spec, rootfs, outAbs)
	}
	if _, err := exec.LookPath("xorriso"); err != nil {
		logWarn("xorriso not found — falling back to a rootfs tarball only.")
		return packageRootfsFallback(spec, rootfs, outAbs)
	}
	isoPath := filepath.Join(outAbs, spec.Name+".iso")
	logStep("Building ISO with grub-mkrescue: " + isoPath)
	if !runVisible("grub-mkrescue", "-o", isoPath, rootfs) {
		logWarn("grub-mkrescue failed — falling back to a rootfs tarball only.")
		return packageRootfsFallback(spec, rootfs, outAbs)
	}
	logDone("Wrote " + isoPath)
	return writeChecksumFile(isoPath)
}

// packageQcow2 builds a real, bootable(-ish) raw disk image and converts it
// to qcow2: partition table + ext4 filesystem + rootfs copy + grub-install
// targeting that disk, then `qemu-img convert`. Every external tool it
// needs is checked up front, with a clear fallback to a plain rootfs
// tarball if any of them is missing — this path needs loop-device and
// mount privileges and has NOT been exercised against a real boot (no VM
// available to test in during development); treat it as a best-effort
// implementation, not a verified one.
func packageQcow2(spec *BuildSpec, rootfs, outAbs string) error {
	logWarn("qcow2 output is experimental: partitioning + grub-install + qemu-img convert, not yet verified against an actual boot.")

	required := []string{"qemu-img", "sfdisk", "losetup", "mkfs.ext4", "grub-install", "partprobe"}
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			logWarn(fmt.Sprintf("'%s' not found — falling back to a rootfs tarball only.", tool))
			return packageRootfsFallback(spec, rootfs, outAbs)
		}
	}

	rawPath := filepath.Join(outAbs, spec.Name+".raw")
	qcowPath := filepath.Join(outAbs, spec.Name+".qcow2")
	sizeMB := estimateImageSizeMB(rootfs)

	logStep(fmt.Sprintf("Creating %dMB raw disk image: %s", sizeMB, rawPath))
	if !runVisible("qemu-img", "create", "-f", "raw", rawPath, fmt.Sprintf("%dM", sizeMB)) {
		return fmt.Errorf("qemu-img create failed")
	}
	defer os.Remove(rawPath) // only the qcow2 is the deliverable; drop the intermediate raw file

	logStep("Partitioning (single bootable ext4 partition + BIOS boot partition for grub)")
	sfdiskScript := "label: gpt\n" +
		"start=2048,size=2048,type=21686148-6449-6E6F-744E-656564454649,name=\"BIOS boot\"\n" +
		"start=4096,type=0FC63DAF-8483-4772-8E79-3D69D8477DE4,name=\"root\"\n"
	sfdiskCmd := exec.Command("sfdisk", rawPath)
	sfdiskCmd.Stdin = strings.NewReader(sfdiskScript)
	sfdiskCmd.Stdout, sfdiskCmd.Stderr = os.Stdout, os.Stderr
	if err := sfdiskCmd.Run(); err != nil {
		return fmt.Errorf("sfdisk failed: %w", err)
	}

	loopDev, err := setupLoopDevice(rawPath)
	if err != nil {
		return fmt.Errorf("losetup failed: %w", err)
	}
	defer runVisible("losetup", "-d", loopDev)
	runVisible("partprobe", loopDev)

	rootPart := loopDev + "p2"
	logStep("Formatting root partition ext4: " + rootPart)
	if !runVisible("mkfs.ext4", "-F", "-L", "root", rootPart) {
		return fmt.Errorf("mkfs.ext4 failed")
	}

	mountPoint, err := os.MkdirTemp("", "isolator-builder-mnt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mountPoint)

	if !runVisible("mount", rootPart, mountPoint) {
		return fmt.Errorf("mount of root partition failed")
	}
	defer runVisible("umount", mountPoint)

	logStep("Copying rootfs into the disk image")
	if err := copyTree(rootfs, mountPoint); err != nil {
		return fmt.Errorf("copying rootfs into image: %w", err)
	}

	logStep("Installing GRUB onto the disk image: " + loopDev)
	if !runVisible("grub-install",
		"--target=i386-pc",
		"--boot-directory="+filepath.Join(mountPoint, "boot"),
		"--modules=part_gpt ext2",
		loopDev) {
		logWarn("grub-install reported errors — the image was still produced but may not boot")
	}

	runVisible("umount", mountPoint)
	runVisible("losetup", "-d", loopDev)

	logStep("Converting raw image to qcow2: " + qcowPath)
	if !runVisible("qemu-img", "convert", "-f", "raw", "-O", "qcow2", rawPath, qcowPath) {
		return fmt.Errorf("qemu-img convert failed")
	}
	logDone("Wrote " + qcowPath)
	return writeChecksumFile(qcowPath)
}

// setupLoopDevice attaches path to a free loop device and returns its path
// (e.g. "/dev/loop7"), using `losetup --show -f` so we get the assigned
// device name back directly instead of parsing `losetup -a`.
func setupLoopDevice(path string) (string, error) {
	cmd := exec.Command("losetup", "--show", "-f", "-P", path)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// estimateImageSizeMB sizes the raw disk as rootfs contents + 30% headroom
// + a fixed 512MB floor, so small rootfs's still get enough slack for
// package manager metadata/logs/growth.
func estimateImageSizeMB(rootfs string) int {
	var total int64
	filepath.Walk(rootfs, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	mb := int(total/(1024*1024)) + 64 // +64MB for partition table + boot
	mb = mb + mb*3/10                 // +30% headroom
	if mb < 512 {
		mb = 512
	}
	return mb
}

func packageRootfsFallback(spec *BuildSpec, rootfs, outAbs string) error {
	tarPath := filepath.Join(outAbs, spec.Name+"-rootfs.tar.gz")
	logStep("Packaging rootfs tarball: " + tarPath)
	if !runVisible("tar", "-C", rootfs, "-czf", tarPath, ".") {
		return fmt.Errorf("tar packaging failed")
	}
	logDone("Wrote " + tarPath)
	return writeChecksumFile(tarPath)
}

// writeChecksumFile computes the SHA-256 of path and writes it as a
// "<hex>  <filename>\n" sidecar (the same sha256sum-compatible format
// Isolator itself uses for package-list.json.sha256), so anyone
// distributing a built image can let recipients verify it wasn't corrupted
// or tampered with in transit.
func writeChecksumFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	sidecar := path + ".sha256"
	line := fmt.Sprintf("%s  %s\n", sum, filepath.Base(path))
	if err := os.WriteFile(sidecar, []byte(line), 0644); err != nil {
		return err
	}
	logDone("Checksum written: " + sidecar)
	return nil
}

// ---------------------------------------------------------------------------
// small utilities
// ---------------------------------------------------------------------------

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.Remove(target)
			return os.Symlink(linkTarget, target)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func boolLabel(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}

func runVisible(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run() == nil
}

func runVisibleErr(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
