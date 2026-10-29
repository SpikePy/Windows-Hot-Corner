# Details

Background on [Windows-Hot-Corner](README.md): how it works, how to
configure and build it, and what it can't do.

## Configuration

`config.yaml` lives in `%LOCALAPPDATA%\HotCorner`, next to the installed
exe, and is created with defaults and a comment per setting the first
time the app runs. The tray menu's **Configure** opens it in whatever
application Windows associates with `.yaml`.

| Setting     | Default    | Meaning                                                        |
| ----------- | ---------- | -------------------------------------------------------------- |
| `enabled`   | `true`     | Whether the hot corners are active, same as the tray toggle    |
| `hot_zones` | `top-left` | Comma-separated corners and edges that open Task View, or `none` |
| `delay_ms`  | `100`      | Milliseconds the pointer has to rest in a zone first, 0-5000   |
| `autostart` | `true`     | Whether the app starts when you sign in (see below)            |

`hot_zones` takes any of `top-left`, `top-right`, `bottom-left`,
`bottom-right` (corners) and `top`, `bottom`, `left`, `right` (whole
edges), e.g. `hot_zones: top-left, bottom-right`. Names are
case-insensitive and `_` works in place of `-`. A corner counts within
8 pixels of it along either edge, so you don't need to hit its exact
pixel. Corners win over edges; with an edge enabled but not its corner,
the corner's area counts as part of the edge.

While `autostart` is `true`, the app makes sure the Startup folder holds
exactly one `HotCorner.lnk`, pointing at
`%LOCALAPPDATA%\HotCorner\HotCorner.exe`; while it's `false`, the app
deletes that shortcut. It checks at launch and again whenever
`config.yaml` changes. The shortcut always points at the installed exe,
never at a copy run from somewhere else, and isn't created at all if the
installed exe doesn't exist.

Unknown keys are ignored, so a file written by another version still
loads, and an unreadable value (an unknown zone name, a delay out of
range) falls back to that setting's default rather than stopping the app.
Edits are picked up within a couple of seconds. Toggling from the tray
writes `enabled` back, preserving your comments.

### Command-line flags

Every setting also has a flag, which wins for that run - config reloads
included - without touching the file:

| Flag                        | Meaning                                                   |
| --------------------------- | --------------------------------------------------------- |
| `-enabled=false`            | Start with the hot corners turned off                     |
| `-hot-zones "top-left,right"` | Use these zones instead of `hot_zones`                  |
| `-delay-ms 250`             | Use this delay instead of `delay_ms`                      |
| `-autostart=false`          | Use this instead of `autostart`                           |
| `-enable-logging`           | Append diagnostics to `HotCorner.log` next to the exe     |

Logging is off by default and is only meant for troubleshooting.

## Tray icon

The icon is the taskbar's Task View glyph - an outlined window behind a
filled, accented one - drawn in code at runtime rather than loaded from an
image file (`internal/taskviewicon` defines the shape,
`internal/tray/icon.go` paints it). The outline is mid-grey so it reads on
both light and dark taskbars. While the hot corners are off, the same
glyph is drawn grey with a diagonal red strike.

Left-click toggles the hot corners. The right-click menu has **Enable**,
**Disable**, **Configure** and **Exit**; the menu is built fresh each time
it opens, with a checkmark on whichever of Enable/Disable is active.

Explorer drops every tray icon when it restarts or crashes, so the tray
package listens for the `TaskbarCreated` broadcast and adds the icon back.

## How it works

- A `WH_MOUSE_LL` low-level mouse hook sees every pointer move
  system-wide without polling. It never changes or swallows input.
- On each move, the hook asks Windows which monitor the pointer is on
  (`MonitorFromPoint`, `GetMonitorInfo`). Most moves are nowhere near an
  edge and end there. On an edge pixel it also checks whether another
  monitor lies just beyond each side: only sides with nothing beyond them
  are hot, because that's where the pointer stops.
- `internal/hotzone` turns that into a zone (`Hit`) and decides when to
  act (`Tracker`): entering a zone starts a timer for `delay_ms`, leaving
  first cancels it, and once Task View opened the pointer has to leave
  every zone before it can trigger again, so resting in a corner never
  flickers Task View open and shut.
- When the timer fires, the app presses `Win+Tab` with `SendInput`, which
  opens Task View (or closes it if it's already open). The timer is a
  thread timer dispatched by the app's own message loop, so nothing runs
  inside the hook that could make Windows drop it for being slow.
- While any mouse button is held, no zone counts, so dragging a window to
  the top edge to maximise it, or a selection to an edge, never opens
  Task View.
- The process is per-monitor DPI aware, so pointer and monitor
  coordinates are real pixels on every screen, including scaled ones.
- Everything runs on one OS thread with a Win32 message loop; the config
  file watcher is the only goroutine, and it only stores new settings in
  atomics and posts a message to the tray window.

## Repository layout

```
cmd/hotcorner/            the tray app
cmd/hotcorner-setup/      Setup: the task dialog (dialog.go) and -mode
internal/hotzone/         which corner/edge a point is in, and when to open Task View
internal/hook/            the low-level mouse hook, delay timer and Win+Tab
internal/tray/            tray icon, menu, runtime-drawn glyph
internal/taskviewicon/    the glyph's geometry, shared with tools/genicon
internal/config/          config.yaml loading, writing and watching
internal/win32/           Win32 declarations shared between packages
internal/setup/           install/uninstall, WinINet download, release links, countdowns
internal/shortcut/        the Startup shortcut, shared by the app and Setup
internal/singleinstance/  named-mutex guard
internal/applog/          opt-in log file next to the exe
tools/genicon/            renders the glyph to an .ico
```

The decision logic (`hotzone`, `config`, `taskviewicon`, `genicon`, and
Setup's release-link parsing and countdowns in `internal/setup`) has no
OS dependency and is covered by table tests that run anywhere; the
Windows-only packages are covered by vet and a cross-compile.

## Building

Requires Go 1.23+. From the repo root:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui -s -w" -o HotCorner.exe ./cmd/hotcorner
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui -s -w" -o Setup_HotCorner.exe ./cmd/hotcorner-setup
```

(`-H=windowsgui` makes both GUI programs, so neither opens a console
window.) There is no cgo dependency, so it cross-compiles cleanly from
Linux/macOS as well as natively on Windows. To show a real version
instead of `dev` in the tray tooltip, add `-X main.version=v1.2.3` to
`-ldflags` (the release workflow does this from the pushed tag).

Checks, the same ones CI runs:

```sh
gofmt -l .
go test ./...
GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...
```

`-unsafeptr=false` is deliberate: the `unsafe.Pointer` conversions of
LPARAM hook payloads and `CreateDIBSection`'s output buffer are addresses
supplied by Win32, outside anything Go's object graph tracks, which vet's
heuristic can't tell apart from a real misuse.

### Icon and manifest

Both exes carry the same file icon, generated from the same glyph the tray
uses and committed as a `rsrc_windows_amd64.syso` per `cmd` directory,
which `go build` links in automatically. Setup's also embeds
[`cmd/hotcorner-setup/setup.manifest`](cmd/hotcorner-setup/setup.manifest),
which turns on current-looking controls (common controls v6) and
per-monitor DPI awareness. To regenerate after changing
`internal/taskviewicon` or the manifest:

```sh
go run ./tools/genicon taskview.ico
go run github.com/akavel/rsrc@latest -ico taskview.ico -arch amd64 -o cmd/hotcorner/rsrc_windows_amd64.syso
go run github.com/akavel/rsrc@latest -ico taskview.ico -manifest cmd/hotcorner-setup/setup.manifest -arch amd64 -o cmd/hotcorner-setup/rsrc_windows_amd64.syso
```

A test in `tools/genicon` fails if either `.syso` no longer matches the
glyph or the manifest.

## Running

You can run `HotCorner.exe` directly from anywhere, or use the Setup
program below to install it and start it at sign-in. Only one instance
runs at a time: a named mutex makes a second copy show a message and exit.

## Setup tool (`cmd/hotcorner-setup`)

`Setup_HotCorner.exe` opens a Windows task dialog with the app's icon and
three buttons:

- **Install/Update** (the default) downloads the newest GitHub release
  into `%LOCALAPPDATA%\HotCorner`, adds a shortcut to your Startup folder
  (`FOLDERID_Startup`, written through the shell's `IShellLink`) if
  `config.yaml` says `autostart: true` - or removes it if not - and, only
  while autostart is on, starts it.
- **Uninstall** removes the Startup shortcut, stops the running app and
  deletes the installed directory, `config.yaml` included.
- **Close** changes nothing. Escape and the title-bar X do the same.

The first page counts down "Installing/updating automatically in 5 s...";
if no button is clicked by then, Install/Update runs on its own. A
progress page follows, then a result page. After a success it counts down
"Closing in 5 s..." and closes itself; after an error it shows the error
and stays open.

Downloads go through **WinINet**, Windows' own HTTP stack, so the system
proxy settings and certificate store apply. Setup never calls the GitHub
API (60 anonymous requests an hour per IP): it downloads
`https://github.com/SpikePy/Windows-Hot-Corner/releases/latest/download/HotCorner.exe`
and learns the version by requesting `.../releases/latest` without
following the redirect, reading the tag from the `Location` header.

Flags for scripted use. With `-mode`, no dialog opens and progress is
printed to the console Setup was started from:

| Flag                 | Meaning                                                       |
| -------------------- | ------------------------------------------------------------- |
| `-mode install`      | Install or update without the dialog or countdowns            |
| `-mode uninstall`    | Uninstall without the dialog or countdowns                    |
| `-install-dir DIR`   | Use DIR instead of `%LOCALAPPDATA%\HotCorner`                 |
| `-no-launch`         | Install, but don't start it now even with autostart on        |
| `-no-autostart`      | Set `autostart: false` in `config.yaml` (install only)        |
| `-keep-files`        | Uninstall, but leave the installed files in place             |

## Releases

- [`ci.yml`](.github/workflows/ci.yml) runs on every push and pull request
  to `main`: tests, vet and a cross-compile, publishing nothing.
- [`build.yml`](.github/workflows/build.yml) runs on `v*` tags: the same
  checks, then builds both exes with the tag stamped in as the version and
  attaches them to a GitHub Release.

## Limitations

- Windows only (10 / 11), amd64.
- Task View is opened by pressing `Win+Tab`; if another tool remaps that
  shortcut, the hot corners follow it.
- Does not run elevated, and does not need to - but while a window running
  as Administrator is in the foreground, Windows' User Interface Privilege
  Isolation may keep the hook from seeing the pointer or the `Win+Tab`
  from getting through.
- It doesn't tell full-screen games or videos apart: a hot corner works
  there too. Use the tray toggle, or an edge you don't reach by accident.
