# Isolator Store (H#)

GTK4 storefront for [Isolator](https://github.com/HackerOS-Linux-System/Isolator):
search `package-list.json`, show details, and install/remove packages by
shelling out to the `isolator` CLI.

This is a rewrite of the original Vala/GTK4/libadwaita/meson `store/` into
[H#](https://github.com/HackerOS-Linux-System/H-Sharp), built with
[bytes](https://github.com/HackerOS-Linux-System/bytes) and the
[`gtk`](https://github.com/Bytes-Repository/gtk) bytes package (`use "bytes -> gtk" from "gtk"`).

## Building

```bash
bytes build          # reads Bytes.hk, fetches the `gtk` dependency, builds
./build/isolator-store
```

Requires `libgtk-4`, `libglib-2.0`, `libgobject-2.0` on the system (see the
`gtk` package's own README for per-distro install commands) and the
`isolator` CLI on `PATH`.

## File map (old → new)

| Vala (removed)                  | H# (this rewrite)         |
|----------------------------------|----------------------------|
| `src/package.vala`               | `src/package.h#`           |
| `src/isolator-client.vala`       | `src/isolator_client.h#`   |
| `src/package-row.vala`           | folded into `src/window.h#` (`build_package_row`) |
| `src/window.vala`                | `src/window.h#`            |
| `src/main.vala`                  | `src/main.h#`              |
| `meson.build`                    | `Bytes.hk`                 |
| `data/window.ui`, `data/store.gresource.xml` | dropped — see below |
| `data/org.hackeros.IsolatorStore.desktop` | unchanged, carried over as-is |

## What changed, and why

**No GtkBuilder/.ui/.gresource.** `bytes -> gtk` doesn't bind
GtkBuilder or GResource — every example that ships with it builds its UI
directly in code, so `window.h#` does the same. `data/window.ui` and
`data/store.gresource.xml` are gone; the layout they described is rebuilt
with plain function calls in `window.h#`.

**No libadwaita, no GApplication.** `bytes -> gtk` deliberately covers
plain GTK4 only. Rather than bind an entire second widget library for one
app, the libadwaita/GApplication pieces of the original UI were rebuilt
from what the library already has:

| Original (libadwaita / GtkApplication) | This rewrite |
|---|---|
| `Adw.Application` / `Adw.ApplicationWindow` | plain `gtk::Window` + a hand-rolled `gtk::App` loop (same pattern as the library's own examples) |
| `Adw.ToastOverlay` / `Adw.Toast` | the existing `status_label` shows the same message as text |
| `Adw.StatusPage` | a centered `gtk::Box` of two `gtk::Label`s (`build_status_placeholder` in `window.h#`) — no icon, since the binding only loads images from a file, not a themed icon name |
| `Adw.HeaderBar` + sidebar-toggle `GtkToggleButton` | a plain horizontal `gtk::Box` (search entry + refresh button) at the top of the window; the sidebar has no collapse toggle |
| `GtkPaned` (draggable split) | a plain horizontal `gtk::Box` with the sidebar pinned to a fixed width |

**The `gtk` package itself was extended, minimally, twice** — both
additions are plain GTK4 (in scope for what the library already covers),
in `gtk_ffi.h#` + `gtk.h#`, not app-specific code:

- `ListBox::clear()` — the binding had no way to remove a widget's
  existing children at all. Added `gtk_widget_get_first_child` /
  `gtk_widget_get_next_sibling` and built `clear()` on top, needed because
  every search keystroke has to empty and rebuild the results list.
- `TextView::set_text()` — there was no access to a `GtkTextView`'s
  buffer at all. Added `gtk_text_view_get_buffer` /
  `gtk_text_buffer_set_text`. It replaces the whole buffer contents at
  once (no incremental append, no scroll-to-end) — see the note below on
  the log view.

**Catalog JSON parsing is hand-rolled, in `isolator_client.h#` only.**
H#'s `std -> json` only supports flat, single-level objects (no arrays at
all — see its own doc comment). `package-list.json` is a top-level array
of objects, so `isolator_client.h#` carries a small array-of-flat-objects
scanner instead. This is app code, not a std-library change, and mirrors
what the original Vala already did for `installed.hk` ("a full .hk parser
is overkill here").

**Install/remove/refresh run synchronously, not streamed.** The original
used GLib `Subprocess` + `DataInputStream` to stream `isolator`'s output
into the log view line-by-line while staying responsive. H#'s
`std -> process` only exposes a blocking `run_args` (no line-by-line
streaming subprocess). So `run_operation` in `window.h#` runs `isolator`
to completion and drops the full captured output into the log view in one
call — **the window will not repaint or respond to input for as long as
the underlying command takes.** For `install`/`remove` this can be a
real wait. Fixing this properly would mean adding a non-blocking,
pollable subprocess primitive to H#'s `std -> process` (or a way to hook
custom per-frame work into `gtk::App::run`'s loop) — a real std/library
change, not a small one, so it wasn't done here; flagging it instead.
