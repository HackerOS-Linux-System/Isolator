package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// `isolator <file.hk>` — declarative, per-project development environments.
//
// Unlike `install`, which puts one package's binary on your PATH forever,
// an environment file describes an entire *project* toolchain — packages,
// env vars, working directory — and gets "activated" on demand, the way
// `flox activate` or `nix develop` do. Under the hood it's still just an
// Isolator container, but it's dedicated to the project, keyed by name, and
// its home is bind-mounted to your project directory instead of $HOME so
// dotfiles/build caches created inside it live right next to your code.
//
// Example `dev.hk`:
//
//   [environment]
//   -> name    => myproject
//   -> distro  => debian
//   -> shell   => bash
//
//   [packages]
//   -> python3
//   -> nodejs
//   -> git
//
//   [env]
//   -> DATABASE_URL => postgres://localhost/dev
//   -> NODE_ENV     => development
//
// Run `isolator dev.hk` from the project directory (or point at the file
// from anywhere) to build the environment once and drop into an activated
// shell; running it again just re-activates instantly.
// ---------------------------------------------------------------------------

type EnvSpec struct {
	Name       string
	Distro     string
	Shell      string
	Packages   []string
	EnvVars    map[string]string
	FilePath   string
	ProjectDir string
}

func envContainerName(name string) string {
	return "isolator-env-" + name
}

// ParseEnvFile loads and validates a project environment definition.
func ParseEnvFile(path string) (*EnvSpec, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	doc, err := LoadHKFile(abs)
	if err != nil {
		return nil, err
	}
	if err := ResolveInterpolations(doc); err != nil {
		return nil, err
	}

	env := doc.Section("environment")
	name := hkGetString(env, "name", "")
	if name == "" {
		// Fall back to the file's base name, e.g. "dev.hk" -> "dev".
		name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	if err := ValidatePackageName(name); err != nil {
		return nil, fmt.Errorf("[environment] -> name: %w", err)
	}
	distro := hkGetString(env, "distro", "debian")
	if _, ok := Distros[distro]; !ok {
		return nil, fmt.Errorf("[environment] -> distro: unknown distro %q", distro)
	}
	shell := hkGetString(env, "shell", "bash")

	var packages []string
	pkgSec := doc.Section("packages")
	for _, k := range pkgSec.Keys() {
		if err := ValidatePackageName(k); err != nil {
			return nil, fmt.Errorf("[packages]: %w", err)
		}
		packages = append(packages, k)
	}

	envVars := map[string]string{}
	envSec := doc.Section("env")
	for _, k := range envSec.Keys() {
		v, _ := envSec.Get(k)
		s, err := v.AsString()
		if err != nil {
			return nil, fmt.Errorf("[env] -> %s: value must be string-like", k)
		}
		envVars[k] = s
	}

	return &EnvSpec{
		Name:       name,
		Distro:     distro,
		Shell:      shell,
		Packages:   packages,
		EnvVars:    envVars,
		FilePath:   abs,
		ProjectDir: filepath.Dir(abs),
	}, nil
}

// HandleEnvFile builds (if needed) and activates the environment described
// by an .hk file, dropping the user into an interactive shell inside it.
func HandleEnvFile(path string) {
	spec, err := ParseEnvFile(path)
	if err != nil {
		PrintError("Failed to parse environment file: " + err.Error())
		return
	}

	d := Distros[spec.Distro]
	contName := envContainerName(spec.Name)

	PrintInfo(fmt.Sprintf("Environment %s  [distro: %s | project: %s]",
		BoldStyle.Render(spec.Name), CyanStyle.Render(spec.Distro), DimStyle.Render(spec.ProjectDir)))

	firstBuild := !ContainerExists(contName)
	if firstBuild {
		PrintStep("Creating environment container (bind-mounted to your project dir)...")
		if !CreateContainer(contName, d.Image, spec.ProjectDir, "cli", d.InitSystem) {
			PrintError(fmt.Sprintf("Failed to create environment container '%s'", contName))
			return
		}
		if !InitContainer(contName, d) {
			PrintWarn("Package manager init returned non-zero (may be OK for some distros)")
		}
	} else if !EnsureContainerRunning(contName) {
		PrintError(fmt.Sprintf("Failed to start environment container '%s'", contName))
		return
	}

	if len(spec.Packages) > 0 {
		installed := installedInEnv(contName)
		var missing []string
		for _, p := range spec.Packages {
			if !installed[p] {
				missing = append(missing, p)
			}
		}
		if len(missing) > 0 {
			PrintInfo("Installing: " + strings.Join(missing, ", "))
			cmd := d.Adapter.Install() + " " + strings.Join(missing, " ")
			if !ExecInContainer(contName, cmd, false, true) {
				PrintError("Failed to install one or more packages into the environment")
				return
			}
			markInstalledInEnv(contName, spec.Packages)
		} else {
			PrintInfo("All packages already present — activating instantly")
		}
	}

	PrintSuccess(fmt.Sprintf("Environment '%s' ready. Activating shell...", spec.Name))

	args := []string{"exec", "-it", "--workdir", "/home/user"}
	for k, v := range spec.EnvVars {
		args = append(args, "--env", k+"="+v)
	}
	args = append(args, contName, spec.Shell)
	ExecCommand(podmanBin, args)
}

// installedInEnv reads the small marker file the environment writes inside
// the container after a successful install batch, to avoid re-running the
// package manager (and re-downloading everything) on every activation.
func installedInEnv(cont string) map[string]bool {
	out, ok := ExecInContainerWithOutput(cont, "cat /etc/isolator-env-installed 2>/dev/null", false)
	result := map[string]bool{}
	if !ok || out == "" {
		return result
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result[line] = true
		}
	}
	return result
}

func markInstalledInEnv(cont string, packages []string) {
	content := strings.Join(packages, "\\n")
	ExecInContainer(cont, fmt.Sprintf("printf '%%b' \"%s\" > /etc/isolator-env-installed", content), false, false)
}

// IsEnvFile reports whether args look like `isolator <something>.hk` rather
// than a known subcommand — used by main.go to route bare .hk files.
func IsEnvFile(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	if !strings.HasSuffix(args[0], ".hk") {
		return "", false
	}
	if _, err := os.Stat(args[0]); err != nil {
		return "", false
	}
	return args[0], true
}
