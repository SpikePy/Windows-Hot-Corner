//go:build windows

// Command hotcorner runs in the background and opens Task View - the
// overview of all open windows and virtual desktops, the same as Win+Tab
// or the taskbar's Task View button - when the mouse pointer rests in one
// of the screen corners or edges configured in config.yaml (top-left by
// default).
//
// A tray icon (the taskbar's Task View glyph = hot corners active, the
// same glyph greyed out with a diagonal red strike = turned off) lets the
// user switch them off without stopping the process: left-click toggles,
// right-click opens an Enable/Disable/Configure/Exit menu. Configure opens
// config.yaml in whatever application Windows has associated with .yaml
// files.
//
// This program does not exit on its own. To stop it: the tray menu's Exit,
// Task Manager, taskkill, or the Setup program (which does it
// automatically when updating).
//
// Logging is OFF by default. Pass -enable-logging to write diagnostics to
// HotCorner.log next to the exe, for troubleshooting only.
//
// Built with -ldflags "-H=windowsgui" so it never shows a console window;
// everything below runs inside a top-level recover() that never lets a
// panic surface as a crash dialog - diagnostics go only to the log file.
package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/SpikePy/Windows-Hot-Corner/internal/applog"
	"github.com/SpikePy/Windows-Hot-Corner/internal/config"
	"github.com/SpikePy/Windows-Hot-Corner/internal/hook"
	"github.com/SpikePy/Windows-Hot-Corner/internal/hotzone"
	"github.com/SpikePy/Windows-Hot-Corner/internal/shortcut"
	"github.com/SpikePy/Windows-Hot-Corner/internal/singleinstance"
	"github.com/SpikePy/Windows-Hot-Corner/internal/tray"
	"github.com/SpikePy/Windows-Hot-Corner/internal/win32"
)

// version is stamped in at build time via -ldflags "-X main.version=...";
// left as "dev" for local/manual builds.
var version = "dev"

const (
	appName     = "Hot Corner"
	mutexName   = `Local\HotCorner_SingleInstance`
	logFileName = "HotCorner.log"

	// exeName is the installed exe, next to config.yaml - what the Startup
	// shortcut points at.
	exeName = "HotCorner.exe"
)

// Tray menu item IDs. 0 is reserved for "nothing selected".
const (
	menuEnable = iota + 1
	menuDisable
	menuConfigure
	menuExit
)

// overrides records which settings were given on the command line: they
// win over config.yaml for the whole run, reloads included.
type overrides struct {
	zones, delay, push, autostart bool
}

func main() {
	// Win32 hooks, windows, timers and the message queue are bound to the
	// OS thread that creates them; the Go runtime must never migrate this
	// goroutine to a different one mid-run.
	runtime.LockOSThread()
	win32.SetDPIAware()

	cfg, cfgErr := config.Load()

	enabled := flag.Bool("enabled", cfg.Enabled, "whether the hot corners are active on launch (overrides config.yaml)")
	zonesFlag := flag.String("hot-zones", cfg.HotZones.String(), "corners/edges that open Task View, e.g. \"top-left, right\" (overrides config.yaml)")
	delayFlag := flag.Int("delay-ms", int(cfg.Delay.Milliseconds()), "milliseconds the pointer has to rest in a hot zone (overrides config.yaml)")
	pushFlag := flag.Int("push-px", int(cfg.Push), "pixels the mouse has to be pushed past the screen edge in a hot zone (overrides config.yaml)")
	autostart := flag.Bool("autostart", cfg.Autostart, "start with Windows (overrides config.yaml)")
	enableLogging := flag.Bool("enable-logging", false, "write diagnostics to "+logFileName+" next to the exe")
	flag.Parse()

	var set overrides
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "hot-zones":
			set.zones = true
		case "delay-ms":
			set.delay = true
		case "push-px":
			set.push = true
		case "autostart":
			set.autostart = true
		}
	})

	logf, logPath := applog.New(logFileName, *enableLogging)
	if cfgErr != nil {
		logf("WARNING loading config.yaml (falling back to defaults): %v", cfgErr)
	}

	zones, err := hotzone.Parse(*zonesFlag)
	if err != nil {
		win32.ErrorBox(appName, fmt.Sprintf("-hot-zones: %v", err))
		return
	}
	delay, err := config.ParseDelay(fmt.Sprint(*delayFlag))
	if err != nil {
		win32.ErrorBox(appName, fmt.Sprintf("-delay-ms: %v", err))
		return
	}

	push, err := config.ParsePush(fmt.Sprint(*pushFlag))
	if err != nil {
		win32.ErrorBox(appName, fmt.Sprintf("-push-px: %v", err))
		return
	}

	release, alreadyRunning, err := singleinstance.Acquire(mutexName)
	if err != nil {
		logf("EXCEPTION acquiring single-instance mutex: %v", err)
		return
	}
	if alreadyRunning {
		logf("Another instance is already running - exiting.")
		win32.ErrorBox(appName, appName+" is already running (check the system tray).")
		return
	}
	defer release()

	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v", r)
		}
	}()

	a := &app{logf: logf, overrides: set, autostart: *autostart}
	a.enabled.Store(*enabled)
	a.zones.Store(uint32(zones))
	a.delay.Store(int64(delay))
	a.push.Store(push)
	a.applyAutostart(*autostart)

	if err := a.run(); err != nil {
		logf("EXCEPTION %v", err)
		win32.ErrorBox(appName, err.Error())
		return
	}
	logf("Exited cleanly. Log at: %s", logPath)
}

// app owns the tray icon and the shared settings. Every method except the
// config watcher's callback runs on the single OS thread that pumps the
// message loop.
type app struct {
	logf      func(format string, args ...any)
	hwnd      uintptr
	overrides overrides
	autostart bool // the command-line value, used while overrides.autostart

	// The settings are read by the mouse hook on every move and written
	// by the tray, the menu and the config watcher, so they're atomic.
	enabled atomic.Bool
	zones   atomic.Uint32 // a hotzone.Zone
	delay   atomic.Int64  // a time.Duration
	push    atomic.Int32

	iconEnabled  uintptr
	iconDisabled uintptr
}

func (a *app) run() error {
	var err error
	if a.iconEnabled, err = tray.EnabledIcon(); err != nil {
		return fmt.Errorf("building the tray icon: %w", err)
	}
	defer tray.DestroyIconHandle(a.iconEnabled)
	if a.iconDisabled, err = tray.DisabledIcon(); err != nil {
		return fmt.Errorf("building the disabled tray icon: %w", err)
	}
	defer tray.DestroyIconHandle(a.iconDisabled)

	if a.hwnd, err = tray.NewWindow(appName, a.toggleEnabled, a.showMenu, a.refreshIcon); err != nil {
		return fmt.Errorf("creating the tray window: %w", err)
	}
	defer tray.DestroyWindow(a.hwnd)

	if err := a.showIcon(); err != nil {
		return fmt.Errorf("adding the tray icon: %w", err)
	}
	defer tray.RemoveIcon(a.hwnd)

	h := hook.New(hook.Settings{
		Enabled: a.enabled.Load,
		Zones:   func() hotzone.Zone { return hotzone.Zone(a.zones.Load()) },
		Delay:   func() time.Duration { return time.Duration(a.delay.Load()) },
		Push:    a.push.Load,
	}, a.logf)
	if err := h.Install(); err != nil {
		return fmt.Errorf("installing the mouse hook: %w", err)
	}
	defer h.Uninstall()

	// Without the watcher the hot corners still work; config edits just
	// wait for the next start.
	if err := config.Watch(a.configChanged); err != nil {
		a.logf("WARNING watching config.yaml: %v", err)
	}

	a.logf("Running %s (enabled=%t, hot_zones=%v, delay=%v, push=%dpx)", version, a.enabled.Load(),
		hotzone.Zone(a.zones.Load()), time.Duration(a.delay.Load()), a.push.Load())
	win32.RunMessageLoop()
	return nil
}

// showIcon puts the icon matching the current state in the tray, with the
// app name, version and enabled/disabled state in its tooltip.
func (a *app) showIcon() error {
	icon, state := a.iconEnabled, "enabled"
	if !a.enabled.Load() {
		icon, state = a.iconDisabled, "disabled"
	}
	return tray.SetIcon(a.hwnd, icon, fmt.Sprintf("%s %s – %s", appName, version, state))
}

// refreshIcon updates the tray icon, called on the message-loop thread
// after the state changed anywhere.
func (a *app) refreshIcon() {
	if err := a.showIcon(); err != nil {
		a.logf("WARNING updating the tray icon: %v", err)
	}
}

// applyAutostart adds or removes the Startup shortcut to the installed exe
// to match the autostart setting. A failure is only logged: the hot
// corners work either way.
func (a *app) applyAutostart(on bool) {
	path, err := config.Path()
	if err == nil {
		err = shortcut.Autostart(on, filepath.Join(filepath.Dir(path), exeName))
	}
	if err != nil {
		a.logf("WARNING updating autostart=%t: %v", on, err)
	}
}

// configChanged runs on the watcher's goroutine, so it only stores the new
// settings and asks the message-loop thread to redraw the icon. Settings
// given on the command line keep their value.
func (a *app) configChanged(cfg config.Config) {
	if a.overrides.autostart {
		cfg.Autostart = a.autostart
	}
	a.applyAutostart(cfg.Autostart)
	if !a.overrides.zones {
		a.zones.Store(uint32(cfg.HotZones))
	}
	if !a.overrides.delay {
		a.delay.Store(int64(cfg.Delay))
	}
	if !a.overrides.push {
		a.push.Store(cfg.Push)
	}
	a.logf("config.yaml changed: enabled=%t, hot_zones=%v, delay=%v, push=%dpx", cfg.Enabled,
		hotzone.Zone(a.zones.Load()), time.Duration(a.delay.Load()), a.push.Load())
	if cfg.Enabled == a.enabled.Load() {
		return
	}
	a.enabled.Store(cfg.Enabled)
	win32.PostMessage(a.hwnd, win32.WMConfigChanged, 0, 0)
}

func (a *app) toggleEnabled() { a.setEnabled(!a.enabled.Load()) }

// setEnabled updates the state the hook reads, writes it back to
// config.yaml so the tray and the file never disagree, and refreshes the
// icon.
func (a *app) setEnabled(enabled bool) {
	a.enabled.Store(enabled)
	if err := config.SetEnabled(enabled); err != nil {
		a.logf("WARNING saving enabled=%t to config.yaml: %v", enabled, err)
		win32.ErrorBox(appName, fmt.Sprintf("The hot corners are now %s, but saving that to config.yaml failed:\n%v",
			map[bool]string{true: "on", false: "off"}[enabled], err))
	}
	a.refreshIcon()
}

func (a *app) showMenu() {
	enabled := a.enabled.Load()
	switch tray.ShowMenu(a.hwnd, []tray.MenuItem{
		{ID: menuEnable, Label: "Enable", Checked: enabled},
		{ID: menuDisable, Label: "Disable", Checked: !enabled},
		{},
		{ID: menuConfigure, Label: "Configure"},
		{},
		{ID: menuExit, Label: "Exit"},
	}) {
	case menuEnable:
		a.setEnabled(true)
	case menuDisable:
		a.setEnabled(false)
	case menuConfigure:
		a.configure()
	case menuExit:
		win32.Quit()
	}
}

// configure makes sure config.yaml exists, then opens it for editing.
func (a *app) configure() {
	path, err := config.Ensure()
	if err != nil {
		a.logf("WARNING creating config.yaml: %v", err)
		win32.ErrorBox(appName, fmt.Sprintf("Couldn't create the config file:\n%v", err))
		return
	}
	if err := win32.OpenWithDefaultApp(a.hwnd, path); err != nil {
		a.logf("WARNING opening %s: %v", path, err)
		win32.ErrorBox(appName, fmt.Sprintf("Couldn't open an editor for:\n%s", path))
	}
}
