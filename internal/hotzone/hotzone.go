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

// Hit returns the zone of enabled the point (x, y) on monitor m is in, or
// None. Corners win over edges; with a corner not enabled, its area still
// counts for the edges it lies on.
func Hit(x, y int32, m Rect, open Open, enabled Zone) Zone {
	if x < m.Left || x >= m.Right || y < m.Top || y >= m.Bottom {
		return None
	}
	atLeft := open.Left && x == m.Left
	atRight := open.Right && x == m.Right-1
	atTop := open.Top && y == m.Top
	atBottom := open.Bottom && y == m.Bottom-1
	nearLeft := x < m.Left+CornerSize
	nearRight := x >= m.Right-CornerSize
	nearTop := y < m.Top+CornerSize
	nearBottom := y >= m.Bottom-CornerSize

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

// Tracker turns the stream of zones the pointer is in into "open Task
// View now" decisions. Task View opens once the pointer has stayed in a
// zone for Delay, and only once per visit: it has to leave every zone
// before it can trigger again, so resting in a corner never flickers Task
// View open and shut.
type Tracker struct {
	zone  Zone // the zone the pointer is in, None outside
	fired bool // Task View was opened during this visit
}

// Move records that the pointer is now in zone z (None when it's in no
// zone). arm reports that a new visit started and the caller should
// (re)start its delay timer; disarm that the pointer left before the
// timer ran out and the caller should stop it.
func (t *Tracker) Move(z Zone) (arm, disarm bool) {
	if z == t.zone {
		return false, false
	}
	prev, fired := t.zone, t.fired
	t.zone = z
	switch {
	case z == None:
		t.fired = false
		return false, prev != None && !fired
	case fired:
		// Sliding along an edge into a corner (or the other way round)
		// is still the same visit.
		return false, false
	default:
		return true, false
	}
}

// Elapsed is called when the delay timer runs out, and reports whether
// Task View should open now.
func (t *Tracker) Elapsed() bool {
	if t.zone == None || t.fired {
		return false
	}
	t.fired = true
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
