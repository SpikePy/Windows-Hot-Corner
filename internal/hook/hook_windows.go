//go:build windows

// Package hook installs the low-level mouse hook that watches the pointer
// for the configured hot corners and edges, and opens Task View by
// pressing Win+Tab when it rests in one. Deciding what counts as a hit is
// internal/hotzone's job; this package only feeds it real coordinates and
// acts on its answers.
//
// A WH_MOUSE_LL hook sees every pointer move system-wide without polling,
// and never changes or swallows any input: every event is passed on.
package hook

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/SpikePy/Windows-Hot-Corner/internal/hotzone"
)

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procSetWindowsHookExW   = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = modUser32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = modUser32.NewProc("GetAsyncKeyState")
	procMonitorFromPoint    = modUser32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW     = modUser32.NewProc("GetMonitorInfoW")
	procSetTimer            = modUser32.NewProc("SetTimer")
	procKillTimer           = modUser32.NewProc("KillTimer")
	procSendInput           = modUser32.NewProc("SendInput")
)

const (
	whMouseLL = 14
	hcAction  = 0

	wmMouseMove = 0x0200

	monitorDefaultToNull    = 0
	monitorDefaultToNearest = 2

	vkLButton = 0x01
	vkRButton = 0x02
	vkMButton = 0x04
	vkTab     = 0x09
	vkLWin    = 0x5B

	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
)

// msllhookstruct mirrors the Win32 MSLLHOOKSTRUCT layout used by
// WH_MOUSE_LL hook callbacks.
type msllhookstruct struct {
	X, Y        int32
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

// keyboardInput is a Win32 INPUT holding a KEYBDINPUT, padded to the
// 40 bytes the union (sized by MOUSEINPUT) takes on x64.
type keyboardInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

// Settings is what the hook asks before acting on any move, so the tray
// and config.yaml can change them while it runs.
type Settings struct {
	Enabled func() bool
	Zones   func() hotzone.Zone
	Delay   func() time.Duration
	Push    func() int32
}

// Hook is an installed mouse hook. Create one with New and install it on
// the thread that runs the message loop; everything below runs on that
// thread, so nothing needs locking.
type Hook struct {
	handle   windows.Handle
	callback uintptr
	timerCB  uintptr
	timer    uintptr // the pending delay timer's ID, 0 if none
	settings Settings
	logf     func(format string, args ...any)
	tracker  hotzone.Tracker
	armedAt  time.Time // when the pending timer was started, for the log
}

// New returns a hook that opens Task View for the zones and delay
// settings reports, while it says the hot corners are enabled.
func New(settings Settings, logf func(format string, args ...any)) *Hook {
	h := &Hook{settings: settings, logf: logf}
	h.callback = syscall.NewCallback(h.proc)
	h.timerCB = syscall.NewCallback(h.timerProc)
	return h
}

// Install registers the hook with Windows. It must be called on a thread
// that pumps a message loop, and that thread must keep running for the
// hook to keep receiving input - and for the delay timer, a thread timer
// dispatched by the same loop, to fire.
func (h *Hook) Install() error {
	r0, _, err := procSetWindowsHookExW.Call(whMouseLL, h.callback, 0, 0)
	if r0 == 0 {
		return fmt.Errorf("mouse hook: %w", err)
	}
	h.handle = windows.Handle(r0)
	return nil
}

// Uninstall removes the hook and any pending timer. Safe to call if it was
// never installed.
func (h *Hook) Uninstall() {
	h.stopTimer()
	if h.handle != 0 {
		procUnhookWindowsHookEx.Call(uintptr(h.handle))
		h.handle = 0
	}
}

// proc is the WH_MOUSE_LL hook procedure. It must return quickly: Windows
// drops a hook that runs past LowLevelHooksTimeout. So it only works out
// which zone the pointer is in and (dis)arms the delay timer; Task View is
// opened from the timer, outside the hook.
func (h *Hook) proc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) == hcAction && wParam == wmMouseMove {
		ms := (*msllhookstruct)(unsafe.Pointer(lParam))
		hit, near, push := h.zonesAt(ms.X, ms.Y)
		h.tracker.Push = h.settings.Push()
		arm, disarm := h.tracker.Move(hit, near, push)
		switch {
		case arm:
			h.armedAt = time.Now()
			h.logf("Pointer in %v at (%d, %d), pushed %d px, opening Task View in %v", hit, ms.X, ms.Y, h.tracker.Pushed(), h.settings.Delay())
			h.startTimer(hotzone.Timer(h.settings.Delay()))
		case disarm:
			h.logf("Pointer left at (%d, %d) after %v, cancelled", ms.X, ms.Y, time.Since(h.armedAt).Round(time.Millisecond))
			h.stopTimer()
		}
	}
	r0, _, _ := procCallNextHookEx.Call(uintptr(h.handle), nCode, wParam, lParam)
	return r0
}

// zonesAt returns the enabled hot zones at (x, y) as hotzone.Hit and
// hotzone.Near see them, and how far past the screen
// edge the point is (see hotzone.Clamp) - or None and 0 while the hot
// corners are off, or a mouse button is held, so dragging a window or a
// selection to the screen edge never opens Task View.
func (h *Hook) zonesAt(x, y int32) (hit, near hotzone.Zone, push int32) {
	zones := h.settings.Zones()
	if zones == hotzone.None || !h.settings.Enabled() || buttonHeld() {
		return hotzone.None, hotzone.None, 0
	}
	mon, _, _ := procMonitorFromPoint.Call(packPoint(x, y), monitorDefaultToNearest)
	if mon == 0 {
		return hotzone.None, hotzone.None, 0
	}
	mi := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return hotzone.None, hotzone.None, 0
	}
	m := hotzone.Rect(mi.rcMonitor)
	// Most moves are nowhere near an edge; only look for neighbouring
	// monitors when the pointer is close to one.
	const d = hotzone.RearmDistance + hotzone.CornerSize
	if x > m.Left+d && x < m.Right-1-d && y > m.Top+d && y < m.Bottom-1-d {
		return hotzone.None, hotzone.None, 0
	}
	// Where the pointer actually ends up, for the neighbour checks.
	px, py, _ := hotzone.Clamp(x, y, m, hotzone.Open{})
	open := hotzone.Open{
		Left:   !monitorAt(m.Left-1, py),
		Right:  !monitorAt(m.Right, py),
		Top:    !monitorAt(px, m.Top-1),
		Bottom: !monitorAt(px, m.Bottom),
	}
	x, y, push = hotzone.Clamp(x, y, m, open)
	return hotzone.Hit(x, y, m, open, zones), hotzone.Near(x, y, m, open, zones), push
}

// monitorAt reports whether any monitor covers (x, y).
func monitorAt(x, y int32) bool {
	mon, _, _ := procMonitorFromPoint.Call(packPoint(x, y), monitorDefaultToNull)
	return mon != 0
}

// packPoint passes a POINT by value the way the x64 calling convention
// does: both 32-bit halves in one register.
func packPoint(x, y int32) uintptr {
	return uintptr(uint32(x)) | uintptr(uint32(y))<<32
}

func buttonHeld() bool {
	for _, vk := range []uintptr{vkLButton, vkRButton, vkMButton} {
		if r, _, _ := procGetAsyncKeyState.Call(vk); r&0x8000 != 0 {
			return true
		}
	}
	return false
}

// startTimer (re)starts the delay timer as a thread timer, which the
// message loop dispatches straight to timerProc.
func (h *Hook) startTimer(d time.Duration) {
	h.stopTimer()
	id, _, err := procSetTimer.Call(0, 0, uintptr(d.Milliseconds()), h.timerCB)
	if id == 0 {
		h.logf("WARNING SetTimer: %v", err)
		return
	}
	h.timer = id
}

func (h *Hook) stopTimer() {
	if h.timer != 0 {
		procKillTimer.Call(0, h.timer)
		h.timer = 0
	}
}

// timerProc runs when the pointer has rested in a zone for the delay.
func (h *Hook) timerProc(hwnd, msg, id, tick uintptr) uintptr {
	h.stopTimer()
	// Settings may have changed, or a button been pressed, since the
	// timer was armed; either way this visit is over.
	if h.tracker.Elapsed() && h.settings.Enabled() && !buttonHeld() {
		h.logf("Opening Task View after %v", time.Since(h.armedAt).Round(time.Millisecond))
		if err := openTaskView(); err != nil {
			h.logf("WARNING opening Task View: %v", err)
		}
	}
	return 0
}

// openTaskView presses Win+Tab, the shortcut that opens (or, while open,
// closes) Task View. Win is released after Tab, so it never reaches the
// shell as a lone press that would open the Start menu.
func openTaskView() error {
	key := func(vk uint16, up bool) keyboardInput {
		in := keyboardInput{typ: inputKeyboard, vk: vk}
		if up {
			in.flags = keyeventfKeyUp
		}
		return in
	}
	inputs := []keyboardInput{key(vkLWin, false), key(vkTab, false), key(vkTab, true), key(vkLWin, true)}
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}
