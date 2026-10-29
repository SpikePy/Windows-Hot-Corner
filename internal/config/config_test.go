package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpikePy/Windows-Hot-Corner/internal/hotzone"
)

// useTempDir points Load, Path and SetEnabled at a fresh directory and
// returns the config.yaml path they will use.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	return filepath.Join(dir, dirName, fileName)
}

func TestLoadCreatesDefaultFile(t *testing.T) {
	path := useTempDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != defaults() {
		t.Errorf("first Load = %+v, want defaults %+v", cfg, defaults())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default config.yaml was not created: %v", err)
	}
	for _, want := range []string{"enabled: true", "hot_zones: top-left", "delay_ms: 100", "push_px: 0", "autostart: true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("created config.yaml is missing %q:\n%s", want, data)
		}
	}

	again, err := Load()
	if err != nil || again != defaults() {
		t.Errorf("reloading the generated file = %+v, %v; want defaults, nil", again, err)
	}
}

func TestLoad(t *testing.T) {
	// with returns the defaults changed by edit, so each case only spells
	// out what differs.
	with := func(edit func(*Config)) Config {
		c := defaults()
		edit(&c)
		return c
	}
	tests := []struct {
		name string
		yaml string
		want Config
	}{
		{"enabled true", "enabled: true\n", defaults()},
		{"enabled false", "enabled: false\n", with(func(c *Config) { c.Enabled = false })},
		{"autostart false", "autostart: false\n", with(func(c *Config) { c.Autostart = false })},
		{"indented and spaced", "  enabled:   false  \n", with(func(c *Config) { c.Enabled = false })},
		{"trailing comment", "enabled: false # off for now\n", with(func(c *Config) { c.Enabled = false })},
		{"quoted value", `autostart: "false"` + "\n", with(func(c *Config) { c.Autostart = false })},
		{"hot zones", "hot_zones: top-right, bottom\n", with(func(c *Config) { c.HotZones = hotzone.TopRight | hotzone.Bottom })},
		{"hot zones none", "hot_zones: none\n", with(func(c *Config) { c.HotZones = hotzone.None })},
		{"hot zones with a comment", "hot_zones: left # for now\n", with(func(c *Config) { c.HotZones = hotzone.Left })},
		{"delay", "delay_ms: 250\n", with(func(c *Config) { c.Delay = 250 * time.Millisecond })},
		{"delay zero", "delay_ms: 0\n", with(func(c *Config) { c.Delay = 0 })},
		{"push", "push_px: 30\n", with(func(c *Config) { c.Push = 30 })},
		{"too much push keeps the default", "push_px: 5000\n", defaults()},
		{"comments and blank lines only", "# nothing here\n\n", defaults()},
		{"unknown keys are ignored", "shortcut: ctrl+alt\nenabled: false\n", with(func(c *Config) { c.Enabled = false })},
		{"unreadable value keeps the default", "enabled: maybe\nautostart: 1\nhot_zones: middle\ndelay_ms: soon\n", defaults()},
		{"out-of-range delay keeps the default", "delay_ms: -5\n", defaults()},
		{"too long a delay keeps the default", "delay_ms: 60000\n", defaults()},
		{"one bad value doesn't affect the others", "hot_zones: nowhere\ndelay_ms: 300\n", with(func(c *Config) { c.Delay = 300 * time.Millisecond })},
		{"missing key keeps the default", "other: 1\n", defaults()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useTempDir(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg != tt.want {
				t.Errorf("Load = %+v, want %+v", cfg, tt.want)
			}
		})
	}
}

func TestParsePush(t *testing.T) {
	tests := []struct {
		in      string
		want    int32
		wantErr bool
	}{
		{"0", 0, false},
		{"40", 40, false},
		{"1000", MaxPush, false},
		{"1001", 0, true},
		{"-1", 0, true},
		{"lots", 0, true},
	}
	for _, tt := range tests {
		got, err := ParsePush(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParsePush(%q) = %v, %v; want %v, error %t", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestParseDelay(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"0", 0, false},
		{"150", 150 * time.Millisecond, false},
		{"5000", MaxDelay, false},
		{"5001", 0, true},
		{"-1", 0, true},
		{"1.5", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseDelay(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseDelay(%q) = %v, %v; want %v, error %t", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestSetEnabledPreservesTheRestOfTheFile(t *testing.T) {
	path := useTempDir(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# my notes\nenabled: true\n# trailing comment\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "enabled: false") {
		t.Errorf("SetEnabled(false) did not update the value:\n%s", got)
	}
	for _, keep := range []string{"# my notes", "# trailing comment"} {
		if !strings.Contains(got, keep) {
			t.Errorf("SetEnabled dropped %q:\n%s", keep, got)
		}
	}

	cfg, err := Load()
	if err != nil || cfg.Enabled {
		t.Errorf("Load after SetEnabled(false) = %+v, %v; want Enabled false", cfg, err)
	}
}

func TestSetEnabledAppendsAMissingKey(t *testing.T) {
	path := useTempDir(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# only a comment"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg.Enabled {
		t.Errorf("Load = %+v, %v; want Enabled false", cfg, err)
	}
}

func TestSetAutostartLeavesEnabledAlone(t *testing.T) {
	useTempDir(t)
	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg.Enabled || cfg.Autostart {
		t.Errorf("Load = %+v, %v; want both false", cfg, err)
	}
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || cfg.Enabled || !cfg.Autostart {
		t.Errorf("Load = %+v, %v; want Enabled false, Autostart true", cfg, err)
	}
}

func TestSetInAppendsOnlyWhenMissing(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"replaces in place", "a: 1\nautostart: true # note\n", "a: 1\nautostart: false # note\n"},
		{"appends with a newline", "# x", "# x\nautostart: false\n"},
		{"appends to empty", "", "autostart: false\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(setIn([]byte(tt.in), "autostart", false)); got != tt.want {
				t.Errorf("setIn = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureCreatesTheFileOnce(t *testing.T) {
	path := useTempDir(t)

	got, err := Ensure()
	if err != nil || got != path {
		t.Fatalf("Ensure() = %q, %v; want %q, nil", got, err, path)
	}
	if err := os.WriteFile(path, []byte("enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "enabled: true") {
		t.Error("Ensure overwrote an existing config.yaml")
	}
}
