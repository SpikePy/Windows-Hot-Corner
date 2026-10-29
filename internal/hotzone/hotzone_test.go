package hotzone

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    Zone
		wantErr bool
	}{
		{"top-left", TopLeft, false},
		{"top-left, bottom-right", TopLeft | BottomRight, false},
		{"Top_Right bottom", TopRight | Bottom, false},
		{"left,right,top,bottom", Left | Right | Top | Bottom, false},
		{"none", None, false},
		{"", None, false},
		{"top-left, top-left", TopLeft, false},
		{"middle", None, true},
		{"top-left, sideways", None, true},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("Parse(%q) = %v, %v; want %v, error %t", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestStringRoundTrips(t *testing.T) {
	for _, z := range []Zone{None, TopLeft, TopLeft | Right, TopLeft | TopRight | BottomLeft | BottomRight | Top | Bottom | Left | Right} {
		back, err := Parse(z.String())
		if err != nil || back != z {
			t.Errorf("Parse(%q) = %v, %v; want %v", z.String(), back, err, z)
		}
	}
	if got := (TopLeft | Right).String(); got != "top-left, right" {
		t.Errorf("String() = %q, want %q", got, "top-left, right")
	}
}

func TestHit(t *testing.T) {
	m := Rect{0, 0, 1920, 1080}
	all := Open{true, true, true, true}
	every := TopLeft | TopRight | BottomLeft | BottomRight | Top | Bottom | Left | Right
	tests := []struct {
		name    string
		x, y    int32
		open    Open
		enabled Zone
		want    Zone
	}{
		{"exact top-left corner", 0, 0, all, TopLeft, TopLeft},
		{"near the corner on the top edge", 5, 0, all, TopLeft, TopLeft},
		{"near the corner on the left edge", 0, 7, all, TopLeft, TopLeft},
		{"near the corner but off the edges", 3, 3, all, TopLeft, None},
		{"past the corner size", 8, 0, all, TopLeft, None},
		{"top-right corner", 1919, 0, all, every, TopRight},
		{"bottom-left corner", 0, 1079, all, every, BottomLeft},
		{"bottom-right corner", 1919, 1079, all, every, BottomRight},
		{"corner not enabled", 0, 0, all, TopRight, None},
		{"corner area counts for an enabled edge", 0, 0, all, Top, Top},
		{"corner wins over edge", 0, 0, all, Top | TopLeft, TopLeft},
		{"top edge", 960, 0, all, Top, Top},
		{"bottom edge", 960, 1079, all, Bottom, Bottom},
		{"left edge", 0, 500, all, Left, Left},
		{"right edge", 1919, 500, all, Right, Right},
		{"one pixel in from the edge", 960, 1, all, every, None},
		{"middle of the screen", 960, 540, all, every, None},
		{"edge shared with another monitor", 1919, 500, Open{true, true, false, true}, Right, None},
		{"corner with a shared side", 1919, 0, Open{true, true, false, true}, TopRight, None},
		{"corner with a shared side still counts for the open edge", 1919, 0, Open{true, true, false, true}, TopRight | Top, Top},
		{"outside the monitor", 2000, 0, all, every, None},
		{"nothing enabled", 0, 0, all, None, None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Hit(tt.x, tt.y, m, tt.open, tt.enabled); got != tt.want {
				t.Errorf("Hit(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestNear(t *testing.T) {
	m := Rect{0, 0, 1920, 1080}
	all := Open{true, true, true, true}
	tests := []struct {
		name    string
		x, y    int32
		enabled Zone
		want    Zone
	}{
		{"on the corner", 0, 0, TopLeft, TopLeft},
		{"a few pixels out of the corner", 4, 3, TopLeft, TopLeft},
		{"just within the rearm distance", RearmDistance, RearmDistance, TopLeft, TopLeft},
		{"beyond the rearm distance along the edge", CornerSize + RearmDistance, 0, TopLeft, None},
		{"beyond the rearm distance off the edge", 0, CornerSize + RearmDistance, TopLeft, None},
		{"near an edge", 960, RearmDistance, Top, Top},
		{"beyond an edge's rearm distance", 960, RearmDistance + 1, Top, None},
		{"near a shared edge", 1900, 500, Right, None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			open := all
			if tt.name == "near a shared edge" {
				open.Right = false
			}
			if got := Near(tt.x, tt.y, m, open, tt.enabled); got != tt.want {
				t.Errorf("Near(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestHitOnASecondMonitor(t *testing.T) {
	// A monitor left of the primary one has negative coordinates.
	m := Rect{-1280, -200, 0, 824}
	if got := Hit(-1280, -200, m, Open{Left: true, Top: true, Bottom: true}, TopLeft); got != TopLeft {
		t.Errorf("Hit at the second monitor's top-left = %v, want top-left", got)
	}
}

// step is one call to the Tracker in a scripted sequence: a move, or the
// delay timer running out (elapsed).
type step struct {
	// The zones the pointer is in, as Hit, Hold and Near report them. An
	// unset hold is taken to be hit, an unset near to be hold, since each
	// area contains the one before.
	hit, hold, near Zone
	push            int32
	elapsed         bool
	// What the call should return: arm/disarm for a move, fire for an
	// elapsed timer.
	arm, disarm, fire bool
}

func TestTracker(t *testing.T) {
	tests := []struct {
		name  string
		push  int32
		steps []step
	}{
		{"enter, wait, fire once", 0, []step{
			{hit: TopLeft, arm: true},
			{elapsed: true, fire: true},
			{elapsed: true, fire: false},
		}},
		{"moving within the zone doesn't re-arm", 0, []step{
			{hit: TopLeft, arm: true},
			{hit: TopLeft},
			{elapsed: true, fire: true},
		}},
		{"drifting a few pixels off the zone keeps the delay running", 0, []step{
			{hit: TopLeft, arm: true},
			{hold: TopLeft},
			{hit: TopLeft},
			{hold: TopLeft},
			{elapsed: true, fire: true},
		}},
		{"leaving before the delay cancels", 0, []step{
			{hit: TopLeft, arm: true},
			{near: TopLeft, disarm: true},
			{elapsed: true, fire: false},
		}},
		{"wobbling back into the zone while moving away doesn't fire again", 0, []step{
			{hit: TopLeft, arm: true},
			{elapsed: true, fire: true},
			{near: TopLeft},
			{hit: TopLeft},
			{elapsed: true, fire: false},
			{near: TopLeft},
			{},
			{hit: TopLeft, arm: true},
			{elapsed: true, fire: true},
		}},
		{"leaving after firing re-arms for the next visit", 0, []step{
			{hit: TopLeft, arm: true},
			{elapsed: true, fire: true},
			{},
			{hit: TopLeft, arm: true},
			{elapsed: true, fire: true},
		}},
		{"sliding into another zone after firing stays one visit", 0, []step{
			{hit: Top, arm: true},
			{elapsed: true, fire: true},
			{hit: TopLeft},
			{elapsed: true, fire: false},
		}},
		{"sliding into another zone before firing doesn't restart the delay", 0, []step{
			{hit: Top, arm: true},
			{hit: TopLeft},
			{elapsed: true, fire: true},
		}},
		{"pushing into the corner keeps it open, not toggling", 0, []step{
			{hit: TopLeft, push: 3, arm: true},
			{hit: TopLeft, push: 2},
			{elapsed: true, fire: true},
			{hit: TopLeft, push: 3},
			{hit: TopLeft},
			{elapsed: true, fire: false},
		}},
		{"push needed: touching alone doesn't arm", 20, []step{
			{hit: TopLeft},
			{hit: TopLeft, push: 5},
			{hit: TopLeft, push: 10},
			{elapsed: true, fire: false},
			{hit: TopLeft, push: 5, arm: true},
			{elapsed: true, fire: true},
		}},
		{"push needed: pushing in one go", 20, []step{
			{hit: TopLeft, push: 25, arm: true},
			{elapsed: true, fire: true},
		}},
		{"push needed: drifting away resets the push", 20, []step{
			{hit: TopLeft, push: 15},
			{near: TopLeft},
			{hit: TopLeft, push: 10},
			{elapsed: true, fire: false},
			{hit: TopLeft, push: 10, arm: true},
		}},
		{"moving outside any zone does nothing", 0, []step{
			{},
			{elapsed: true, fire: false},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := Tracker{Push: tt.push}
			for i, s := range tt.steps {
				if s.elapsed {
					if got := tr.Elapsed(); got != s.fire {
						t.Fatalf("step %d: Elapsed() = %t, want %t", i, got, s.fire)
					}
					continue
				}
				hold, near := s.hold, s.near
				if hold == None {
					hold = s.hit
				}
				if near == None {
					near = hold
				}
				arm, disarm := tr.Move(s.hit, hold, near, s.push)
				if arm != s.arm || disarm != s.disarm {
					t.Fatalf("step %d: Move(%v, %v, %v, %d) = %t, %t; want %t, %t", i, s.hit, hold, near, s.push, arm, disarm, s.arm, s.disarm)
				}
			}
		})
	}
}

func TestClamp(t *testing.T) {
	m := Rect{0, 0, 1920, 1080}
	all := Open{true, true, true, true}
	tests := []struct {
		name               string
		x, y               int32
		open               Open
		wantX, wantY, push int32
	}{
		{"inside", 500, 500, all, 500, 500, 0},
		{"on the edge", 0, 500, all, 0, 500, 0},
		{"past the left edge", -3, 500, all, 0, 500, 3},
		{"past the top-left corner", -2, -4, all, 0, 0, 6},
		{"past the bottom-right corner", 1925, 1081, all, 1919, 1079, 8},
		{"past a shared edge", 1925, 500, Open{true, true, false, true}, 1919, 500, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, push := Clamp(tt.x, tt.y, m, tt.open)
			if x != tt.wantX || y != tt.wantY || push != tt.push {
				t.Errorf("Clamp(%d, %d) = %d, %d, %d; want %d, %d, %d", tt.x, tt.y, x, y, push, tt.wantX, tt.wantY, tt.push)
			}
		})
	}
}

func TestHold(t *testing.T) {
	m := Rect{0, 0, 1920, 1080}
	all := Open{true, true, true, true}
	for _, tt := range []struct {
		x, y int32
		want Zone
	}{
		{0, 0, TopLeft},
		{3, 2, TopLeft},
		{HoldDistance, HoldDistance, TopLeft},
		{0, CornerSize + HoldDistance, None},
		{HoldDistance + 1, HoldDistance + 1, None},
	} {
		if got := Hold(tt.x, tt.y, m, all, TopLeft); got != tt.want {
			t.Errorf("Hold(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestTimer(t *testing.T) {
	for _, tt := range []struct{ in, want time.Duration }{
		{-time.Second, 10 * time.Millisecond},
		{0, 10 * time.Millisecond},
		{10 * time.Millisecond, 10 * time.Millisecond},
		{250 * time.Millisecond, 250 * time.Millisecond},
	} {
		if got := Timer(tt.in); got != tt.want {
			t.Errorf("Timer(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
