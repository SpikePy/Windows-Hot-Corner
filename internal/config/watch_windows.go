//go:build windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// settleTime lets a save finish before config.yaml is read: some editors
// empty the file first and write it again a moment later.
const settleTime = 150 * time.Millisecond

// Watch calls onChange with the freshly loaded settings, from a goroutine
// of its own, each time config.yaml is saved. It watches the folder
// rather than the file, since editors often save by replacing the file,
// and ignores everything else in there, such as the log. Nothing polls:
// the goroutine sleeps until Windows reports a change in the folder.
func Watch(onChange func(Config)) error {
	path, err := Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	h, err := windows.FindFirstChangeNotification(dir, false,
		windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_SIZE|windows.FILE_NOTIFY_CHANGE_LAST_WRITE)
	if err != nil {
		return fmt.Errorf("watching %s: %w", dir, err)
	}
	go func() {
		defer windows.FindCloseChangeNotification(h)
		last := stampOf(path)
		for {
			if ev, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			// Re-arm before looking, so a save during the look isn't missed.
			if err := windows.FindNextChangeNotification(h); err != nil {
				return
			}
			if stampOf(path) == last {
				continue
			}
			time.Sleep(settleTime)
			last = stampOf(path)
			if cfg, err := Load(); err == nil {
				onChange(cfg)
			}
		}
	}()
	return nil
}

// stamp tells one saved version of a file from another.
type stamp struct {
	modified int64
	size     int64
}

// stampOf returns path's stamp, or the zero stamp if it doesn't exist.
func stampOf(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{fi.ModTime().UnixNano(), fi.Size()}
}
