# Isolator
Podman-based package manager: installs packages from any supported distro
into isolated (or shared) containers, wires up GUI/GPU/audio access
production-style, and drops a thin wrapper + `.desktop` launcher on the host.

## Commands
- `isolator init` — first-run setup: config file, PATH check, GPU/audio/X11/Wayland detection report
- `isolator install <pkg> [--isolated] [--dry-run]` — install a package
- `isolator remove <pkg> [--force] [--dry-run]` — remove an installed package (blocks removal if another installed package depends on it, unless `--force`)
- `isolator exec <pkg> -- <cmd> [args...]` — run an arbitrary command inside a package's container
- `isolator search <term>` — fuzzy search the repository
- `isolator search all` — list every package in the repository
- `isolator docs` — open the online documentation in your browser
- `isolator info <pkg>` — package details
- `isolator list` — installed packages
- `isolator status` — container status dashboard
- `isolator update` — update packages in all managed containers
- `isolator refresh` — force re-download of the repository list
- `isolator upgrade` — full system upgrade (host + containers)
- `isolator autoremove` — remove orphaned containers with no packages left
- `isolator clean` — prune dangling Podman images/build cache
- `isolator snapshot <container>` / `isolator rollback <container>` / `isolator snapshots` — commit-based rollback points

## Config
`~/.config/isolator/config.json`, created automatically by `isolator init`:

```json
{
  "default_isolated": false,
  "enable_gui": true,
  "gpu_mode": "auto",
  "audio_backend": "auto",
  "gtk_theme": "",
  "icon_theme": "",
  "qt_platform": "gtk3",
  "shm_size": "1g",
  "create_desktop_entries": true,
  "allow_desktop_environments": false,
  "require_checksum": false
}
```

- `gpu_mode`: `auto` | `nvidia` | `amd` | `intel` | `none`
- `audio_backend`: `auto` | `pipewire` | `pulseaudio` | `alsa` | `none`
- `allow_desktop_environments`: opt-in flag needed before a `type: "de"` package gets `--systemd=always` + cgroup access (full desktop environments need this; regular GUI apps don't)
- `require_checksum`: if true, `isolator refresh`/`install` hard-fail when the repo's `.sha256` sidecar is missing, instead of just warning

## Graphics/GPU/audio handling
GUI and DE packages automatically get, based on what's actually detected on
the host:
- X11, with a **per-container scoped Xauthority cookie** (via `xauth
  nlist`/`nmerge`) instead of a blanket `xhost +`
- Wayland, mounting only the specific compositor socket
- PipeWire → PulseAudio → ALSA, in that priority order
- GPU: Intel/AMD via `/dev/dri`; NVIDIA via CDI (`nvidia-container-toolkit`)
  when configured, else a manual device-node fallback
- D-Bus session bus (+ read-only system bus for notifications/UDisks)
- Fonts, GTK/Qt theme env vars, icon theme, and a persisted `dconf` directory
  so apps look and feel native and remember their settings
- `/etc/localtime`, `TZ`, `LANG`/`LC_ALL` and a `1g` `/dev/shm` (Electron apps
  need real shared memory, not the 64MB default)
- A `.desktop` launcher (with a best-effort extracted icon) in
  `~/.local/share/applications`, so installed GUI apps show up in your
  normal application menu

Run `isolator init` any time to see exactly what was detected.

## Security
- Every package name (from the user *and* from the downloaded repository
  JSON) is validated against a strict allow-list before it's ever placed in
  a shell command run inside a container.
- The repository list is fetched over HTTPS with a bounded timeout and,
  when the maintainers publish a `package-list.json.sha256` sidecar,
  verified against it before being trusted.
- `remove` checks whether another installed package in the same
  (non-isolated) container declares the target as a dependency, and refuses
  unless `--force` is passed.

## Package types
Every catalog entry has a `type`:
- `cli` — a command-line tool. Gets a `~/.local/bin` wrapper, no GUI mounts.
- `gui` — a single graphical application. Gets a wrapper + `.desktop`
  launcher + X11/Wayland/audio/GPU/theme mounts (see below).
- `de` — a full desktop environment. Gets everything `gui` gets, plus
  (opt-in via `allow_desktop_environments`) `--systemd=always` and cgroup
  access. No wrapper — there's no single binary to run; `isolator exec
  <name> -- bash` to get inside it.
- `system` — a systemd-managed background service/daemon (init systems,
  bootloaders, display managers, database/web servers, etc). Gets
  `--systemd=always` + cgroup access (opt-in via `allow_system_containers`
  in config.hk — a separate flag from `de`'s, since you may want one
  without the other) but **none** of the GUI/audio/GPU/theme mounts. No
  wrapper; `isolator exec <name> -- systemctl status` to check on it.
- `lib` — a development library (C/C++ headers, `.so`/`.a`, `pkg-config`
  files — e.g. `libssl-dev`, `openssl-devel`, `boost`). No wrapper, no GUI
  mounts. `isolator info <lib>` shows every other cataloged package that
  actually depends on it (same-distro reverse lookup); `install`/`remove`
  recognize these as first-class dependencies rather than opaque strings —
  see `src/deps.go`.

## Supported distros
`debian`, `fedora`, `archlinux`, `opensuse`, `ubuntu`, `slackware`, `blackarch`

- **blackarch** — official `blackarchlinux/blackarch:latest` image, pacman-based
  (reuses the Arch adapter 1:1). Needs `--security-opt seccomp=unconfined`,
  which Isolator adds automatically for this distro. ~360 real BlackArch
  tool packages are in the catalog (recon, exploitation, cracking, forensics,
  reverse engineering, C2 frameworks, cloud/IaC security scanners, and more).

Solus was evaluated and deliberately **not** added: it has no official
Docker Hub image (verified — nothing is published under `docker.io/solus`
or `docker.io/getsolus`), and shipping a distro entry that points at a
placeholder/non-existent image would just be a broken `isolator install`
waiting to happen. Revisit if/when Solus (or a trusted third party)
publishes an official container image.

### A note on package names
Every package name in `package-list.json` must be **globally unique across
the whole catalog** — `isolator install <pkg>` is deliberately a flat,
one-word lookup with no `--distro` flag or `pkg@distro` disambiguation. This
means a given upstream tool (e.g. `ripgrep`) can only be listed once, under
one distro, even though it's packaged identically everywhere. That's a
known, intentional trade-off for a dead-simple install UX, and it's the
main constraint that shapes how new packages get added — favor giving each
new tool a home under whichever distro doesn't have it yet, over listing it
five times.

## isolated (separate tool)
`isolated/` is a fifth standalone piece — a variant of the main `isolator`
CLI (own Go module, own source tree) with one behavioral difference:
**every** install always goes into its own dedicated container and home
directory. There's no `--isolated` flag on this tool because there's
nothing to toggle — that's its only mode. Uses its own state directories
(`~/.config/isolated/`, `~/.isolated/homes/`) and a distinct
`isolated-<distro>-<pkg>` container-naming prefix, so it can never collide
with `isolator --isolated`'s own containers if you have both installed.
Same catalog, same `.hk` config format, same every other command. Full
details in `isolated/README.md`.

```
cd isolated && go build -o isolated .
./isolated install steam
```

## Store (separate tool)
`store/` is a small GTK4 + libadwaita graphical front end for `isolator`,
written in **Vala** with **Meson** — a sixth standalone piece, its own
build system, following the same "zero container-management logic of its
own" rule as `builder`/`daemon`/`isolated`: it reads Isolator's own cached
catalog (`package-list.json`) and state (`installed.hk`) directly, and
calls the real `isolator install`/`remove`/`refresh` for every action.
Full details, including exactly what has and hasn't been verified, in
`store/README.md`.

```
cd store
meson setup build
ninja -C build
./build/isolator-store
```

## Documentation
`isolator docs` opens the online documentation
(https://hackeros-linux-system.github.io/HackerOS-Website/tools-docs/isolator.html)
in your default browser.

## Daemon (separate tool)
`daemon/` is a third standalone tool — its own Go module, own source tree.
It requires `isolator` on `PATH` and has no container-management logic of
its own: it's a scheduler that calls `isolator update` /`autoremove`
/`clean`/`snapshot --all` on configurable intervals, unattended (e.g. as a
systemd service), plus a tiny Unix-socket status/trigger API. Full details
in `daemon/README.md`.

```
cd daemon && go build -o isolator-daemon .
./isolator-daemon run daemon.hk
```

## Builder (separate tool)
`builder/` is a standalone tool — its own Go module, own source tree,
**not** part of `source-code/`. It requires `isolator` (and `podman`) on
`PATH`. It builds minimal base images (kernel + Podman + Isolator itself —
everything else installs afterward as Isolator-managed containers) from a
declarative `.hk` spec, optionally paired with a live-build-inspired
companion directory (`package-lists/`, `hooks/`, `includes.chroot/`). Full
details, including its honestly-documented experimental/partial parts, are
in `builder/README.md`.

```
cd builder && go build -o builder .
./builder build myimage.hk
```

## Development environments (`isolator <file.hk>`)
Beyond installing individual packages, Isolator can activate a whole
declarative, per-project dev environment — closer to `flox activate` or
`nix develop` than to a Dockerfile. It's still backed by a regular Isolator
container, but that container is dedicated to the project, and its home is
bind-mounted to your project directory instead of `$HOME`.

`dev.hk`:
```
[environment]
-> name    => myproject
-> distro  => debian
-> shell   => bash

[packages]
-> python3
-> nodejs
-> git

[env]
-> DATABASE_URL => postgres://localhost/dev
-> NODE_ENV     => development
```

```
isolator dev.hk
```
First run builds the container and installs `[packages]`; every run after
that activates instantly and drops you into `[shell]` with `[env]` exported.

## The .hk format
Every local Isolator config/state file (`config.hk`, `installed.hk`,
`snapshots.hk`, environment files) uses **`.hk`** instead of JSON/YAML — a
small, from-scratch-implemented format (parser + serializer in `src/hk.go`,
spec: https://hackeros-linux-system.github.io/HackerOS-Website/tools-docs/hk.html).
Sections in `[brackets]`, dash-depth nesting (`->`, `-->`, `--->`), dotted-key
shorthand, `! comments`, and `${section.key}` / `${env:VAR}` interpolation
with cycle detection are all supported. The repository's own
`package-list.json` stays JSON, since it's a plain HTTP-distributed
interchange format rather than local Isolator state.

