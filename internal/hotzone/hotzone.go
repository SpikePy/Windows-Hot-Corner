// Package hotzone decides when the mouse pointer has hit one of the
// configured screen corners or edges: which zone a point lies in, and when
// resting there should open Task View. It is pure logic with no OS
// dependency, so the mouse hook (which feeds it real coordinates) stays
// thin and all of this is tested on any platform.
package hotzone

import (
	"fmt"
	"strings"
	"time"
)

// Zone is a set of screen corners and edges, as bit flags.
type Zone uint32

const (
	TopLeft Zone = 1 << iota
	TopRight
	BottomLeft
	BottomRight
	Top
	Bottom
	Left
	Right
)

// None is the empty set: no hot corners at all.
const None Zone = 0

// names lists every zone in the order String prints them, under the name
// config.yaml uses.
var names = []struct {
	zone Zone
	name string
}{
	{TopLeft, "top-left"},
	{TopRight, "top-right"},
	{BottomLeft, "bottom-left"},
	{BottomRight, "bottom-right"},
	{Top, "top"},
	{Bottom, "bottom"},
	{Left, "left"},
	{Right, "right"},
}

// Parse reads a comma- or space-separated list of zone names such as
// "top-left, bottom-right". "none" (or an empty string) is the empty set.
// Names are case-insensitive and "_" works in place of "-".
func Parse(s string) (Zone, error) {
	var z Zone
	for _, field := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		name := strings.ReplaceAll(strings.ToLower(field), "_", "-")
		if name == "none" {
			continue
		}
		found := false
		for _, n := range names {
			if n.name == name {
				z |= n.zone
				found = true
				break
			}
		}
		if !found {
			return None, fmt.Errorf("unknown corner or edge %q", field)
		}
	}
	return z, nil
}

// String returns z in the form Parse reads, e.g. "top-left, right".
func (z Zone) String() string {
	var parts []string
	for _, n := range names {
		if z&n.zone != 0 {
			parts = append(parts, n.name)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// Rect is a monitor's area in screen pixels; Right and Bottom are
// exclusive, as in a Win32 RECT.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Open tells which sides of a monitor are outer edges of the desktop,
// with no other monitor beyond them. Only those can be hot: the pointer
// stops there, while at a side shared with another monitor it just
// passes through.
type Open struct{ Left, Top, Right, Bottom bool }

// CornerSize is how close to a corner, in pixels along the edge, the
// pointer counts as being in it - so a corner is easy to hit without
// landing on its exact pixel.
const CornerSize = 8

// HoldDistance is how far, in pixels, the pointer may drift off a zone
// while its delay runs. A hand resting on the mouse nudges it back a pixel
// or two now and then; that mustn't cancel and restart the delay.
const HoldDistance = 8

// RearmDistance is how far, in pixels, the pointer has to get from the
// zone that opened Task View before it can trigger again. Without it, the
// pointer wobbling back onto the edge as you start moving away would press
// Win+Tab a second time - and close Task View again.
const RearmDistance = 50

// Clamp moves (x, y) onto monitor m and reports how many pixels past its
// outer edges (the open sides) it was. The mouse hook sees the pointer
// where the mouse pushed it, before Windows stops it at the screen edge,
// so pushing into a corner reports points like (-3, 0): those are still
// in the corner, and the overshoot is how hard the pointer is being
// pushed. Past a side shared with another monitor nothing counts, since
// the pointer moves on to that monitor.
func Clamp(x, y int32, m Rect, open Open) (cx, cy, push int32) {
	cx, cy = x, y
	if x < m.Left {
		cx = m.Left
		if open.Left {
			push += m.Left - x
		}
	}
	if x > m.Right-1 {
		cx = m.Right - 1
		if open.Right {
			push += x - (m.Right - 1)
		}
	}
	if y < m.Top {
		cy = m.Top
		if open.Top {
			push += m.Top - y
		}
	}
	if y > m.Bottom-1 {
		cy = m.Bottom - 1
		if open.Bottom {
			push += y - (m.Bottom - 1)
		}
	}
	return cx, cy, push
}

// Hit returns the zone of enabled the point (x, y) on monitor m is in, or
// None. Corners win over edges; with a corner not enabled, its area still
// counts for the edges it lies on.
func Hit(x, y int32, m Rect, open Open, enabled Zone) Zone {
	return within(x, y, m, open, enabled, 0)
}

// Hold is Hit with every zone grown inwards by HoldDistance: the area the
// pointer has to stay in while a zone's delay runs.
func Hold(x, y int32, m Rect, open Open, enabled Zone) Zone {
	return within(x, y, m, open, enabled, HoldDistance)
}

// Near is Hit with every zone grown inwards by RearmDistance: the area the
// pointer has to leave before a zone can trigger again.
func Near(x, y int32, m Rect, open Open, enabled Zone) Zone {
	return within(x, y, m, open, enabled, RearmDistance)
}

// within returns the zone (x, y) is in, counting points up to grow pixels
// in from an edge as on it.
func within(x, y int32, m Rect, open Open, enabled Zone, grow int32) Zone {
	if x < m.Left || x >= m.Right || y < m.Top || y >= m.Bottom {
		return None
	}
	atLeft := open.Left && x <= m.Left+grow
	atRight := open.Right && x >= m.Right-1-grow
	atTop := open.Top && y <= m.Top+grow
	atBottom := open.Bottom && y >= m.Bottom-1-grow
	nearLeft := x < m.Left+CornerSize+grow
	nearRight := x >= m.Right-CornerSize-grow
	nearTop := y < m.Top+CornerSize+grow
	nearBottom := y >= m.Bottom-CornerSize-grow

	// A corner needs both its sides to be outer edges and the pointer on
	// one of them, close to the other.
	corners := []struct {
		zone         Zone
		sidesOpen    bool
		onEdge, near bool
	}{
		{TopLeft, open.Top && open.Left, atTop || atLeft, nearTop && nearLeft},
		{TopRight, open.Top && open.Right, atTop || atRight, nearTop && nearRight},
		{BottomLeft, open.Bottom && open.Left, atBottom || atLeft, nearBottom && nearLeft},
		{BottomRight, open.Bottom && open.Right, atBottom || atRight, nearBottom && nearRight},
	}
	for _, c := range corners {
		if enabled&c.zone != 0 && c.sidesOpen && c.onEdge && c.near {
			return c.zone
		}
	}

	for _, e := range []struct {
		zone Zone
		at   bool
	}{{Top, atTop}, {Bottom, atBottom}, {Left, atLeft}, {Right, atRight}} {
		if enabled&e.zone != 0 && e.at {
			return e.zone
		}
	}
	return None
}

// Tracker turns the stream of pointer positions into "open Task View now"
// decisions. A visit starts when the pointer hits a zone. If Push is set,
// the pointer then has to be pushed that many pixels further against the
// screen edge before the delay starts; the move that brings it into the
// zone doesn't count towards that. The delay keeps running while the
// pointer stays within HoldDistance of a zone (sliding from an edge into
// its corner included), and is cancelled once it drifts further. Task
// View opens once per visit: after that the pointer has to get
// RearmDistance away from every zone before it can trigger again, so
// neither resting in a corner nor wobbling back into it while moving away
// presses Win+Tab a second time, which would close Task View.
type Tracker struct {
	// Push is how many pixels the pointer has to be pushed past the edge
	// before the delay starts; 0 starts it on the first touch.
	Push int32

	state  trackerState
	pushed int32 // pixels pushed so far in this visit
}

type trackerState int

const (
	idle    trackerState = iota // waiting for the pointer to hit a zone
	pushing                     // in a zone, waiting for enough push
	pending                     // pushed enough, the delay is running
	fired                       // Task View opened during this visit
)

// Move records the zones the pointer is now in - hit, hold and near as
// Hit, Hold and Near report them (None outside) - and push, how far past
// the screen edge this move went (see Clamp). arm reports that the caller
// should start its delay timer; disarm that the pointer left before the
// timer ran out and the caller should stop it.
func (t *Tracker) Move(hit, hold, near Zone, push int32) (arm, disarm bool) {
	switch t.state {
	case idle:
		if hit == None {
			return false, false
		}
		// The move that arrives carries the rest of its momentum past
		// the edge - a fast flick into a corner can overshoot by dozens
		// of pixels in one go. Only pushing once the pointer is there
		// counts.
		t.state, t.pushed = pushing, 0
		return t.addPush(0), false
	case pushing:
		if hold == None {
			t.state = idle
			return false, false
		}
		return t.addPush(push), false
	case pending:
		if hold == None {
			t.state = idle
			return false, true
		}
	case fired:
		if near == None {
			t.state = idle
		}
	}
	return false, false
}

// Pushed returns how many pixels the pointer has been pushed past the
// edge during the current visit, for logging.
func (t *Tracker) Pushed() int32 { return t.pushed }

// addPush counts push towards Push, and moves on to pending (reporting
// true, to arm the timer) once there's enough.
func (t *Tracker) addPush(push int32) bool {
	t.pushed += push
	if t.pushed < t.Push {
		return false
	}
	t.state = pending
	return true
}

// Elapsed is called when the delay timer runs out, and reports whether
// Task View should open now.
func (t *Tracker) Elapsed() bool {
	if t.state != pending {
		return false
	}
	t.state = fired
	return true
}

// Timer converts a delay to the period a Win32 timer gets: SetTimer
// clamps anything below 10 ms up to 10 ms anyway, and a negative delay
// makes no sense.
func Timer(delay time.Duration) time.Duration {
	if delay < 10*time.Millisecond {
		return 10 * time.Millisecond
	}
	return delay
}
