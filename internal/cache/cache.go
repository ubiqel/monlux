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

// appDirName names the directory under the user's cache directory. It matches
// the CLI name.
const appDirName = "monlux"

// ErrNotExist means there is no usable cache file. It is returned for a missing
// file and for a corrupted one, which is removed.
var ErrNotExist = errors.New("cache file does not exist")

// Path returns the location of the named cache file, creating its directory.
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

// ReadJSON decodes the named cache file into v. It returns ErrNotExist if the
// file is missing or corrupted.
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

// WriteJSON encodes v as JSON into the named cache file.
func WriteJSON(name string, v any) error {
	path, err := Path(name)
	if err != nil {
		return err
	}

	content, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling to json: %w", err)
	}

	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	return nil
}
