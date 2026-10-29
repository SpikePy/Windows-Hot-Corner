// Package config reads and writes the user-editable config.yaml that
// lives in the same per-user directory the installer uses
// (%LOCALAPPDATA%\HotCorner). It is created with default
// values the first time it's loaded, so the user always has a real file to
// edit rather than having to know the option names up front.
//
// The parser is a deliberately small "key: value" reader rather than a
// YAML library: the file is a handful of flat settings, and keeping it
// dependency-free means a malformed file can never do worse than fall back
// to defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SpikePy/Windows-Hot-Corner/internal/hotzone"
)

// The defaults are both what a fresh config.yaml says and the fallback
// when the file is missing or a value can't be read.
const (
	DefaultEnabled   = true
	DefaultAutostart = true
	DefaultHotZones  = hotzone.TopLeft
	DefaultDelay     = 100 * time.Millisecond
	DefaultPush      = 0
)

// MaxDelay caps delay_ms: a hot corner that needs the pointer to rest for
// longer than this is a typo, not a setting.
const MaxDelay = 5 * time.Second

// MaxPush caps push_px, for the same reason.
const MaxPush = 1000

const (
	dirName  = "HotCorner"
	fileName = "config.yaml"
)

const template = `# HotCorner configuration
#
# Edit and save this file to change how the app behaves. Changes apply as
# soon as you save -- no restart needed.

# enabled: turn the hot corners on or off without uninstalling the app.
# You can also toggle this from the tray icon, and override it for a
# single run with -enabled.
enabled: %t

# hot_zones: where moving the mouse opens Task View (the overview of all
# open windows and virtual desktops). A comma-separated list of any of:
#   top-left, top-right, bottom-left, bottom-right   (corners)
#   top, bottom, left, right                         (whole edges)
# or none. Only outer edges of your screens count - an edge shared with
# another monitor never triggers. Override with -hot-zones.
hot_zones: %s

# delay_ms: how long, in milliseconds, the mouse has to stay in a hot zone
# before Task View opens (0-5000); leaving it earlier cancels. Raise it if it opens by accident.
# Override with -delay-ms.
delay_ms: %d

# push_px: how many pixels further you have to push the mouse against the
# screen edge, once the pointer is in a hot zone, before the delay starts
# (0-1000). The pointer itself stops at the edge; this counts the mouse
# movement past it, so a hot corner only opens when you mean it. 0 = just
# touching the zone is enough. Override with -push-px.
push_px: %d

# autostart: start the app when you sign in to Windows. While this is true
# the app keeps a shortcut to itself in your Startup folder; set it to
# false and that shortcut is removed. Override with -autostart.
autostart: %t
`

// Config holds the settings read from config.yaml. Keys it doesn't know
// are ignored, so a file written by a newer or older version still loads.
type Config struct {
	Enabled   bool
	HotZones  hotzone.Zone
	Delay     time.Duration
	Push      int32
	Autostart bool
}

func defaults() Config {
	return Config{Enabled: DefaultEnabled, HotZones: DefaultHotZones, Delay: DefaultDelay, Push: DefaultPush, Autostart: DefaultAutostart}
}

// defaultFile is the text of a freshly created config.yaml.
func defaultFile() []byte {
	return []byte(fmt.Sprintf(template, DefaultEnabled, DefaultHotZones, DefaultDelay.Milliseconds(), DefaultPush, DefaultAutostart))
}

// ParseDelay reads a delay_ms value, rejecting anything outside 0 to
// MaxDelay.
func ParseDelay(value string) (time.Duration, error) {
	ms, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("delay_ms %q is not a whole number", value)
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 0 || d > MaxDelay {
		return 0, fmt.Errorf("delay_ms %d is outside 0-%d", ms, MaxDelay.Milliseconds())
	}
	return d, nil
}

// Load reads config.yaml, creating it with default values on first run.
// Any error, or an unreadable value, falls back to that field's default
// rather than failing - a bad or missing config file should never stop the
// program from starting.
func Load() (Config, error) {
	def := defaults()

	path, err := Path()
	if err != nil {
		return def, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if werr := os.WriteFile(path, defaultFile(), 0o644); werr != nil {
			return def, fmt.Errorf("writing default config.yaml: %w", werr)
		}
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("reading config.yaml: %w", err)
	}
	return parse(string(data)), nil
}

// ParsePush reads a push_px value, rejecting anything outside 0 to
// MaxPush.
func ParsePush(value string) (int32, error) {
	px, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("push_px %q is not a whole number", value)
	}
	if px < 0 || px > MaxPush {
		return 0, fmt.Errorf("push_px %d is outside 0-%d", px, MaxPush)
	}
	return int32(px), nil
}

// parse reads the settings it recognises out of a config file's text,
// leaving anything it doesn't understand at its default.
func parse(text string) Config {
	cfg := defaults()
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := setting(line)
		if !ok {
			continue
		}
		switch key {
		case "enabled":
			parseBool(value, &cfg.Enabled)
		case "autostart":
			parseBool(value, &cfg.Autostart)
		case "hot_zones":
			if z, err := hotzone.Parse(value); err == nil {
				cfg.HotZones = z
			}
		case "delay_ms":
			if d, err := ParseDelay(value); err == nil {
				cfg.Delay = d
			}
		case "push_px":
			if p, err := ParsePush(value); err == nil {
				cfg.Push = p
			}
		}
	}
	return cfg
}

// parseBool sets *field from "true" or "false", leaving it alone for
// anything else.
func parseBool(value string, field *bool) {
	switch value {
	case "true":
		*field = true
	case "false":
		*field = false
	}
}

// setting splits one line into its key and value, reporting false for
// blank lines, comments and anything that isn't "key: value".
func setting(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, ok = strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	if idx := strings.IndexByte(value, '#'); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`), true
}

// Path returns the config.yaml path, creating its containing directory if
// necessary. It does not create the file itself - see Load and Ensure.
func Path() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		var err error
		if dir, err = os.UserConfigDir(); err != nil {
			return "", fmt.Errorf("resolving user config directory: %w", err)
		}
	}
	dir = filepath.Join(dir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}
	return filepath.Join(dir, fileName), nil
}

// Ensure creates config.yaml with default values if it doesn't exist yet,
// and returns its path either way - what the tray's Configure entry needs
// before handing the path to an editor.
func Ensure() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, defaultFile(), 0o644); err != nil {
			return "", fmt.Errorf("writing default config.yaml: %w", err)
		}
	} else if err != nil {
		return "", err
	}
	return path, nil
}

// SetEnabled saves the enabled setting - see set.
func SetEnabled(enabled bool) error { return set("enabled", enabled) }

// SetAutostart saves the autostart setting - see set.
func SetAutostart(autostart bool) error { return set("autostart", autostart) }

// set updates one boolean setting in place, preserving the rest of the
// file (comments included) so the user's own edits survive a change made
// from the tray or Setup. If the key isn't there, it's appended.
func set(key string, value bool) error {
	path, err := Ensure()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, setIn(data, key, value), 0o644)
}

// setIn returns data with key's value replaced by value, or with
// "key: value" appended if the key isn't set anywhere.
func setIn(data []byte, key string, value bool) []byte {
	re := regexp.MustCompile(`(?m)^([ \t]*` + regexp.QuoteMeta(key) + `[ \t]*:[ \t]*)\S+(.*)$`)
	v := fmt.Sprintf("%t", value)
	if re.Match(data) {
		return re.ReplaceAll(data, []byte("${1}"+v+"$2"))
	}
	out := append([]byte(nil), data...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, []byte(key+": "+v+"\n")...)
}
