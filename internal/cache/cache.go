// Package cache stores monlux's cache files in the user's cache directory. The
// cache is only an optimization, so a file that can't be read or parsed is
// treated as missing.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const appDirName = "monlux"

// ErrNotExist means there is no usable cache file. It is returned for a missing
// file and for a corrupted one, which is removed.
var ErrNotExist = errors.New("cache file does not exist")

func Path(name string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir location: %w", err)
	}
	appCacheDir := filepath.Join(cacheDir, appDirName)
	err = os.MkdirAll(appCacheDir, 0o750)
	if err != nil {
		return "", fmt.Errorf("creating app cache dir: %w", err)
	}

	return filepath.Join(appCacheDir, name), nil
}

func ReadJSON(name string, v any) error {
	path, err := Path(name)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotExist
	}
	if err != nil {
		return fmt.Errorf("reading cache file: %w", err)
	}

	if err := json.Unmarshal(content, v); err != nil {
		slog.Warn("Failed to unmarshal cache file content as JSON.", "error", err, "file", name)
		if err := os.Remove(path); err != nil {
			slog.Warn("Failed to remove file with corrupted cache.", "error", err, "cache_filepath", path)
		}

		return ErrNotExist
	}

	return nil
}

// WriteJSON encodes v as JSON into the named cache file. The content goes to a
// temporary file first and is renamed into place, so a reader never sees a
// partly written file.
func WriteJSON(name string, v any) error {
	path, err := Path(name)
	if err != nil {
		return err
	}

	content, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling to json: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), name+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing cache file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing cache file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing cache file: %w", err)
	}

	return nil
}
