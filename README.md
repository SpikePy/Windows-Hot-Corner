# Windows-Hot-Corner

A tiny Windows-only background utility that opens **Task View** — the
overview of all open windows and virtual desktops, the same as `Win+Tab`
or the taskbar's Task View button — when you move the mouse into a screen
corner or edge.

- Push the pointer into the **top-left corner** (the default) and Task
  View opens. Do it again to close it.
- Pick any corners and edges you like in `config.yaml`: `top-left`,
  `top-right`, `bottom-left`, `bottom-right`, `top`, `bottom`, `left`,
  `right`, or several at once.
- Only outer screen edges count: with several monitors, the edge between
  two of them never triggers, since the pointer just passes through it.
- Nothing triggers while a mouse button is held, so dragging a window or
  a selection to the edge works as usual.

## Install

Download `Setup_HotCorner.exe` from the
[latest release](https://github.com/SpikePy/Windows-Hot-Corner/releases/latest)
and run it. It offers **Install/Update**, **Uninstall** and **Close**; if
you don't click anything within 5 seconds, it installs on its own.
Installing puts the app into `%LOCALAPPDATA%\HotCorner`. While the
`autostart` setting is on (the default), Setup starts it right away and
the app keeps a shortcut to itself in your Startup folder, so it starts
whenever you sign in. Run Setup again any time to update to the newest
release.

Nothing here needs administrator rights, and nothing is installed for
other users.

## Tray icon

The app runs in the system tray with the same two-window glyph as the
taskbar's Task View button, showing its name, version and whether it's
enabled or disabled on hover. **Left-click** the icon to turn the hot
corners on or off; while they're off the icon is grey with a red
strike-through. **Right-click** for a menu with Enable and Disable (the
active one is checked), Configure and Exit.

## Settings

Configure opens `%LOCALAPPDATA%\HotCorner\config.yaml` (the real file has
a comment above each setting):

```yaml
enabled: true          # same as the tray toggle
hot_zones: top-left    # corners/edges that open Task View, comma-separated, or none
delay_ms: 100          # how long the pointer has to rest there first (0-5000)
push_px: 0             # push the mouse this many pixels further against the edge first (0-1000)
autostart: true        # start when you sign in
```

Edits apply as soon as you save the file, no restart needed.

## More

[DETAILS.md](DETAILS.md) covers the command-line flags, how it works,
building from source, the Setup tool, releases and known limitations.
