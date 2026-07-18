package src

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// GPU detection
// ---------------------------------------------------------------------------

// GPUVendor identifies which GPU stack should be wired into the container.
type GPUVendor string

const (
	GPUNone   GPUVendor = "none"
	GPUIntel  GPUVendor = "intel"
	GPUAMD    GPUVendor = "amd"
	GPUNvidia GPUVendor = "nvidia"
	GPUHybrid GPUVendor = "hybrid" // e.g. Intel iGPU + Nvidia dGPU (Optimus)
)

// DetectGPU inspects /sys/class/drm to figure out which vendor driver(s) are
// bound on the host, without shelling out to lspci (which may not be
// installed). It is intentionally conservative: if nothing is found it
// returns GPUNone rather than guessing.
func DetectGPU() GPUVendor {
	hasIntel, hasAMD, hasNvidia := false, false, false

	entries, err := os.ReadDir("/sys/class/drm")
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
				continue
			}
			vendorPath := filepath.Join("/sys/class/drm", name, "device", "vendor")
			data, err := os.ReadFile(vendorPath)
			if err != nil {
				continue
			}
			switch strings.TrimSpace(string(data)) {
			case "0x8086":
				hasIntel = true
			case "0x1002":
				hasAMD = true
			case "0x10de":
				hasNvidia = true
			}
		}
	}

	if !hasNvidia {
		if _, err := os.Stat("/dev/nvidiactl"); err == nil {
			hasNvidia = true
		}
		if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
			hasNvidia = true
		}
	}

	switch {
	case hasNvidia && (hasIntel || hasAMD):
		return GPUHybrid
	case hasNvidia:
		return GPUNvidia
	case hasAMD:
		return GPUAMD
	case hasIntel:
		return GPUIntel
	default:
		return GPUNone
	}
}

// nvidiaCDIAvailable reports whether the NVIDIA Container Toolkit has
// generated a CDI spec, which lets Podman attach the GPU with a single
// `--device nvidia.com/gpu=all` instead of us hand-mounting device nodes and
// driver libraries (fragile and easy to get wrong across driver versions).
func nvidiaCDIAvailable() bool {
	candidates := []string{
		"/etc/cdi/nvidia.yaml",
		"/etc/cdi/nvidia.json",
		"/var/run/cdi/nvidia.yaml",
		"/var/run/cdi/nvidia.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Audio backend detection
// ---------------------------------------------------------------------------

type AudioBackend string

const (
	AudioNone       AudioBackend = "none"
	AudioPipeWire   AudioBackend = "pipewire"
	AudioPulseAudio AudioBackend = "pulseaudio"
	AudioALSA       AudioBackend = "alsa"
)

func DetectAudio(uid int) AudioBackend {
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	if _, err := os.Stat(filepath.Join(runtimeDir, "pipewire-0")); err == nil {
		return AudioPipeWire
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "pulse", "native")); err == nil {
		return AudioPulseAudio
	}
	if _, err := os.Stat("/dev/snd"); err == nil {
		return AudioALSA
	}
	return AudioNone
}

// ---------------------------------------------------------------------------
// X11 scoped authentication
// ---------------------------------------------------------------------------

// generateScopedXauth extracts the MIT-MAGIC-COOKIE for the current
// $DISPLAY into a container-specific Xauthority file instead of relying on
// blanket `xhost +`. The cookie file is only readable by the current user
// and is mounted read-only into the container, so a compromised container
// gains access to exactly one display session and nothing else on the host.
// Falls back to "" (caller then skips XAUTHORITY entirely) if `xauth`
// is not installed or extraction fails — the raw socket mount still works
// for hosts that run with a permissive `xhost` policy.
func generateScopedXauth(contName, display string) string {
	if display == "" {
		return ""
	}
	if _, err := exec.LookPath("xauth"); err != nil {
		return ""
	}
	dir := filepath.Join(os.Getenv("HOME"), configDir, "xauth")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}
	dest := filepath.Join(dir, contName+".Xauthority")

	list := exec.Command("xauth", "nlist", display)
	listOut, err := list.Output()
	if err != nil || len(listOut) == 0 {
		return ""
	}
	merge := exec.Command("xauth", "-f", dest, "nmerge", "-")
	merge.Stdin = strings.NewReader(string(listOut))
	if err := merge.Run(); err != nil {
		return ""
	}
	_ = os.Chmod(dest, 0600)
	return dest
}

// ---------------------------------------------------------------------------
// Building the full set of podman-run arguments for graphical support
// ---------------------------------------------------------------------------

// graphicsContext bundles everything BuildGraphicsArgs needs so it stays a
// pure, testable function instead of reaching into globals.
type graphicsContext struct {
	uid, gid   int
	contName   string
	cfg        Config
	pkgType    string // "cli" | "gui" | "de" | "lib" | "system"
	initSystem string // "systemd" | "sysvinit" | "none" — see Distro.InitSystem
}

// BuildGraphicsArgs returns the extra `podman run` arguments needed to give
// a container working, reasonably-secured access to the host's display,
// audio, GPU, fonts/themes and clock — i.e. everything a modern GUI app
// (Electron, GTK, Qt, SDL, Vulkan/OpenGL) expects to find.
func BuildGraphicsArgs(ctx graphicsContext) []string {
	var args []string

	if ctx.pkgType == "cli" || ctx.pkgType == "lib" {
		return args
	}

	// "system" packages (systemd-managed background services/daemons) get
	// systemd + cgroup access — the same mechanism "de" uses — but none of
	// the display/audio/GPU/theme mounts, since they're headless by design.
	if ctx.pkgType == "system" {
		return buildSystemdArgs(ctx, ctx.cfg.AllowSystemContainers, "system")
	}

	if !ctx.cfg.EnableGUI {
		return args
	}

	args = append(args, buildDisplayArgs(ctx)...)
	args = append(args, buildAudioArgs(ctx)...)
	args = append(args, buildGPUArgs(ctx)...)
	args = append(args, buildDBusArgs(ctx)...)
	args = append(args, buildFontsAndThemeArgs(ctx)...)
	args = append(args, buildMiscDesktopArgs(ctx)...)

	if ctx.pkgType == "de" {
		args = append(args, buildSystemdArgs(ctx, ctx.cfg.AllowDesktopEnvironments, "de")...)
	}

	return args
}

func buildDisplayArgs(ctx graphicsContext) []string {
	var args []string

	if _, err := os.Stat("/tmp/.X11-unix"); err == nil {
		args = append(args, "--volume", "/tmp/.X11-unix:/tmp/.X11-unix:rw")
		display := os.Getenv("DISPLAY")
		if display != "" {
			args = append(args, "--env", "DISPLAY="+display)
			if xauth := generateScopedXauth(ctx.contName, display); xauth != "" {
				args = append(args, "--volume", xauth+":/home/user/.Xauthority:ro")
				args = append(args, "--env", "XAUTHORITY=/home/user/.Xauthority")
			}
		}
	}

	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	if waylandDisplay == "" {
		waylandDisplay = "wayland-0"
	}
	runtimeDir := fmt.Sprintf("/run/user/%d", ctx.uid)
	waylandSock := filepath.Join(runtimeDir, waylandDisplay)
	if _, err := os.Stat(waylandSock); err == nil {
		args = append(args,
			"--volume", fmt.Sprintf("%s:/run/user/%d/%s:rw", waylandSock, ctx.uid, waylandDisplay),
			"--env", "WAYLAND_DISPLAY="+waylandDisplay,
			"--env", fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", ctx.uid),
		)
	}

	return args
}

func buildAudioArgs(ctx graphicsContext) []string {
	var args []string
	backend := AudioBackend(ctx.cfg.AudioBackend)
	if backend == "" || backend == "auto" {
		backend = DetectAudio(ctx.uid)
	}
	runtimeDir := fmt.Sprintf("/run/user/%d", ctx.uid)

	switch backend {
	case AudioPipeWire:
		sock := filepath.Join(runtimeDir, "pipewire-0")
		if _, err := os.Stat(sock); err == nil {
			args = append(args, "--volume", fmt.Sprintf("%s:/run/user/%d/pipewire-0:rw", sock, ctx.uid))
		}
	case AudioPulseAudio:
		sock := filepath.Join(runtimeDir, "pulse")
		if _, err := os.Stat(sock); err == nil {
			args = append(args, "--volume", fmt.Sprintf("%s:/run/user/%d/pulse:rw", sock, ctx.uid))
		}
	case AudioALSA:
		if _, err := os.Stat("/dev/snd"); err == nil {
			args = append(args, "--device", "/dev/snd:/dev/snd")
			args = append(args, "--group-add", "keep-groups")
		}
	}
	return args
}

func buildGPUArgs(ctx graphicsContext) []string {
	var args []string

	vendor := GPUVendor(ctx.cfg.GPUMode)
	if vendor == "" || vendor == "auto" {
		vendor = DetectGPU()
	}
	if vendor == GPUNone {
		return args
	}

	if _, err := os.Stat("/dev/dri"); err == nil {
		args = append(args, "--device", "/dev/dri:/dev/dri")
		args = append(args, "--group-add", "keep-groups")
	}

	if vendor == GPUNvidia || vendor == GPUHybrid {
		if nvidiaCDIAvailable() {
			args = append(args, "--device", "nvidia.com/gpu=all")
		} else {
			for _, dev := range []string{"/dev/nvidia0", "/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/nvidia-uvm-tools", "/dev/nvidia-modeset"} {
				if _, err := os.Stat(dev); err == nil {
					args = append(args, "--device", dev+":"+dev)
				}
			}
			args = append(args,
				"--env", "NVIDIA_VISIBLE_DEVICES=all",
				"--env", "NVIDIA_DRIVER_CAPABILITIES=all",
			)
		}
	}

	return args
}

func buildDBusArgs(ctx graphicsContext) []string {
	var args []string
	runtimeDir := fmt.Sprintf("/run/user/%d", ctx.uid)
	sessionBus := filepath.Join(runtimeDir, "bus")
	if _, err := os.Stat(sessionBus); err == nil {
		args = append(args,
			"--volume", fmt.Sprintf("%s:/run/user/%d/bus:rw", sessionBus, ctx.uid),
			"--env", fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", ctx.uid),
		)
	}
	if _, err := os.Stat("/run/dbus/system_bus_socket"); err == nil {
		args = append(args, "--volume", "/run/dbus/system_bus_socket:/run/dbus/system_bus_socket:ro")
	}
	return args
}

func buildFontsAndThemeArgs(ctx graphicsContext) []string {
	var args []string
	home := os.Getenv("HOME")

	if _, err := os.Stat("/usr/share/fonts"); err == nil {
		args = append(args, "--volume", "/usr/share/fonts:/usr/share/fonts:ro")
	}
	userFonts := filepath.Join(home, ".fonts")
	if _, err := os.Stat(userFonts); err == nil {
		args = append(args, "--volume", userFonts+":/home/user/.fonts:ro")
	}
	userFontsXDG := filepath.Join(home, ".local/share/fonts")
	if _, err := os.Stat(userFontsXDG); err == nil {
		args = append(args, "--volume", userFontsXDG+":/home/user/.local/share/fonts:ro")
	}

	gtkTheme := ctx.cfg.GTKTheme
	if gtkTheme == "" {
		gtkTheme = os.Getenv("GTK_THEME")
	}
	if gtkTheme != "" {
		args = append(args, "--env", "GTK_THEME="+gtkTheme)
	}
	iconTheme := ctx.cfg.IconTheme
	if iconTheme != "" {
		args = append(args, "--env", "GTK_ICON_THEME="+iconTheme)
	}
	if ctx.cfg.QtPlatform != "" {
		args = append(args, "--env", "QT_QPA_PLATFORMTHEME="+ctx.cfg.QtPlatform)
	}

	dconfDir := filepath.Join(home, ".config/dconf")
	if err := os.MkdirAll(dconfDir, 0700); err == nil {
		args = append(args, "--volume", dconfDir+":/home/user/.config/dconf:rw")
	}

	return args
}

func buildMiscDesktopArgs(ctx graphicsContext) []string {
	var args []string

	if _, err := os.Stat("/etc/localtime"); err == nil {
		args = append(args, "--volume", "/etc/localtime:/etc/localtime:ro")
	}
	if tz := os.Getenv("TZ"); tz != "" {
		args = append(args, "--env", "TZ="+tz)
	}
	for _, envVar := range []string{"LANG", "LC_ALL", "LANGUAGE"} {
		if v := os.Getenv(envVar); v != "" {
			args = append(args, "--env", envVar+"="+v)
		}
	}

	shm := ctx.cfg.ShmSize
	if shm == "" {
		shm = "1g"
	}
	args = append(args, "--shm-size", shm)

	return args
}

// buildSystemdArgs adds the privilege set required to run a full init
// system (systemd as PID 1) inside a container: real cgroup access and
// `--systemd=always`. This is opt-in per package type ("de" or "system")
// via its own config flag, because it's a meaningfully bigger trust
// boundary than a single sandboxed application or a systemd-less daemon.
//
// It also checks whether the target distro's image actually ships systemd
// at all (see Distro.InitSystem) — requesting --systemd=always on an image
// that boots BSD-style /etc/rc.d scripts (Slackware) wouldn't fail loudly,
// it would just silently do nothing useful, which is worse than not adding
// the flag and saying so.
func buildSystemdArgs(ctx graphicsContext, allowed bool, kindLabel string) []string {
	if ctx.initSystem != "" && ctx.initSystem != "systemd" {
		PrintWarn(fmt.Sprintf(
			"Package needs systemd (type '%s'), but this distro's image uses '%s', not systemd — skipping --systemd=always since it wouldn't do anything on this image. The package installs, but won't be managed by an init system inside the container.",
			kindLabel, ctx.initSystem))
		return nil
	}
	if !allowed {
		PrintWarn(fmt.Sprintf("Package needs systemd/cgroup access (type '%s'), but that's disabled in config.hk — running with app-level privileges only", kindLabel))
		return nil
	}
	return []string{
		"--systemd", "always",
		"--volume", "/sys/fs/cgroup:/sys/fs/cgroup:rw",
	}
}

// ---------------------------------------------------------------------------
// Desktop launcher (.desktop) + icon extraction
// ---------------------------------------------------------------------------

func desktopEntryDir() string {
	return filepath.Join(os.Getenv("HOME"), ".local/share/applications")
}

func iconCacheDir() string {
	return filepath.Join(os.Getenv("HOME"), ".local/share/icons/isolator")
}

// ExtractIcon best-effort copies an icon for pkg out of the container into
// a host-side icon cache, returning the path (or "" if none was found, in
// which case callers should fall back to a generic icon name).
func ExtractIcon(cont, pkg string) string {
	candidates, ok := ExecInContainerWithOutput(cont,
		fmt.Sprintf("find /usr/share/icons /usr/share/pixmaps -iname '*%s*' -type f 2>/dev/null | head -n1", pkg), false)
	if !ok || candidates == "" {
		return ""
	}
	if err := os.MkdirAll(iconCacheDir(), 0755); err != nil {
		return ""
	}
	ext := filepath.Ext(candidates)
	if ext == "" {
		ext = ".png"
	}
	dest := filepath.Join(iconCacheDir(), pkg+ext)
	if !ExecCommand(podmanBin, []string{"cp", cont + ":" + candidates, dest}) {
		return ""
	}
	return dest
}

// GenerateDesktopEntry writes a .desktop launcher pointing at the wrapper
// script in ~/.local/bin, so GUI/DE packages show up in the host's
// application menu exactly like natively installed software.
func GenerateDesktopEntry(pkg, contName, pkgType string) error {
	if pkgType != "gui" && pkgType != "de" {
		return nil
	}
	if err := os.MkdirAll(desktopEntryDir(), 0755); err != nil {
		return err
	}

	icon := ExtractIcon(contName, pkg)
	if icon == "" {
		icon = "application-x-executable"
	}

	wrapperPath := filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s (Isolator)
Comment=Installed via Isolator in container %s
Exec=%s %%U
Icon=%s
Terminal=false
Categories=Utility;
X-Isolator-Container=%s
`, pkg, contName, wrapperPath, icon, contName)

	dest := filepath.Join(desktopEntryDir(), "isolator-"+pkg+".desktop")
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(entry), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// RemoveDesktopEntry deletes the launcher and cached icon created for pkg.
func RemoveDesktopEntry(pkg string) {
	_ = os.Remove(filepath.Join(desktopEntryDir(), "isolator-"+pkg+".desktop"))
	entries, err := os.ReadDir(iconCacheDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), pkg+".") {
			_ = os.Remove(filepath.Join(iconCacheDir(), e.Name()))
		}
	}
}

// ---------------------------------------------------------------------------
// Human-readable report, used by `isolator init` / `isolator status`
// ---------------------------------------------------------------------------

// PrintGPUReport prints what Isolator auto-detected, so users understand
// exactly what a GUI install will wire up before they run one.
func PrintGPUReport() {
	uid := os.Getuid()
	gpu := DetectGPU()
	audio := DetectAudio(uid)

	fmt.Println()
	fmt.Println(SectionStyle.Render("  Graphics & Audio Detection"))
	fmt.Printf("    GPU:   %s\n", gpuLabel(gpu))
	fmt.Printf("    Audio: %s\n", audioLabel(audio))
	if _, err := os.Stat("/tmp/.X11-unix"); err == nil {
		fmt.Printf("    X11:   %s\n", SuccessStyle.Render("available"))
	} else {
		fmt.Printf("    X11:   %s\n", DimStyle.Render("not found"))
	}
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	if waylandDisplay == "" {
		waylandDisplay = "wayland-0"
	}
	if _, err := os.Stat(filepath.Join(fmt.Sprintf("/run/user/%d", uid), waylandDisplay)); err == nil {
		fmt.Printf("    Wayland: %s\n", SuccessStyle.Render("available ("+waylandDisplay+")"))
	} else {
		fmt.Printf("    Wayland: %s\n", DimStyle.Render("not found"))
	}
	if gpu == GPUNvidia || gpu == GPUHybrid {
		if nvidiaCDIAvailable() {
			fmt.Printf("    NVIDIA CDI: %s\n", SuccessStyle.Render("configured — GPU passthrough will use it"))
		} else {
			fmt.Printf("    NVIDIA CDI: %s\n", WarnStyle.Render("not configured — falling back to manual device mounts"))
			fmt.Println(DimStyle.Render("      Install nvidia-container-toolkit and run 'nvidia-ctk cdi generate' for a more robust setup."))
		}
	}
	fmt.Println()
}

func gpuLabel(v GPUVendor) string {
	switch v {
	case GPUNvidia:
		return SuccessStyle.Render("NVIDIA")
	case GPUAMD:
		return SuccessStyle.Render("AMD")
	case GPUIntel:
		return SuccessStyle.Render("Intel")
	case GPUHybrid:
		return SuccessStyle.Render("Hybrid (Intel/AMD + NVIDIA)")
	default:
		return DimStyle.Render("none detected")
	}
}

func audioLabel(a AudioBackend) string {
	switch a {
	case AudioPipeWire:
		return SuccessStyle.Render("PipeWire")
	case AudioPulseAudio:
		return SuccessStyle.Render("PulseAudio")
	case AudioALSA:
		return SuccessStyle.Render("ALSA (direct /dev/snd)")
	default:
		return DimStyle.Render("none detected")
	}
}

// itoa is a tiny local helper kept so gui.go doesn't need callers elsewhere
// to import strconv just for this file's needs.
func itoa(i int) string { return strconv.Itoa(i) }
