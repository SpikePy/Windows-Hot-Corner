package taskviewicon

import "testing"

func TestAtParts(t *testing.T) {
	tests := []struct {
		name string
		x, y int
		want Part
	}{
		{"front window centre", 11, 20, PartFront},
		{"back window frame, top edge", 20, 4, PartBack},
		{"back window frame, right edge", 27, 10, PartBack},
		{"inside the back window's frame", 22, 9, PartNone},
		{"gap between the windows", 21, 15, PartNone},
		{"rounded-off corner of the front window", 3, 28, PartNone},
		{"top-left of the canvas", 0, 0, PartNone},
		{"bottom-right of the canvas", 31, 31, PartNone},
		{"outside the canvas", 32, 32, PartNone},
		{"negative", -1, 4, PartNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := At(tt.x, tt.y); got != tt.want {
				t.Errorf("At(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

func TestAtMatchesAtScaledAtGridSize(t *testing.T) {
	for y := 0; y < GridSize; y++ {
		for x := 0; x < GridSize; x++ {
			if got, want := At(x, y), AtScaled(x, y, GridSize); got != want {
				t.Fatalf("At(%d, %d) = %v, AtScaled = %v", x, y, got, want)
			}
		}
	}
}

// The glyph has to stay recognisable at tray size: both windows keep real
// area at every size the icons are rendered at.
func TestGlyphCoverage(t *testing.T) {
	for _, size := range []int{16, 24, 32, 48, 256} {
		counts := map[Part]int{}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				counts[AtScaled(x, y, size)]++
			}
		}
		total := size * size
		if pct := counts[PartFront] * 100 / total; pct < 15 || pct > 40 {
			t.Errorf("size %d: front window covers %d%% of the canvas, want 15-40%%", size, pct)
		}
		if pct := counts[PartBack] * 100 / total; pct < 5 || pct > 30 {
			t.Errorf("size %d: back window frame covers %d%% of the canvas, want 5-30%%", size, pct)
		}
	}
}
