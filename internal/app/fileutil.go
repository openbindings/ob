package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func localPathFileURL(path string) (string, error) {
	drive := len(path) >= 2 && path[1] == ':' && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z'))
	if (drive && (runtime.GOOS != "windows" || !filepath.IsAbs(path))) || strings.HasPrefix(path, `\\`) {
		return "", fmt.Errorf("file path must be local and rooted; drive-relative, device and remote-share paths are unsupported")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return (&url.URL{Scheme: "file", Path: abs}).String(), nil
}

func fileURLLocalPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || u.User != nil || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) {
		return "", fmt.Errorf("only local file URIs are supported; remote file authorities and opaque paths are unavailable")
	}
	path := u.Path // already percent-decoded; a literal %2F must remain data
	if runtime.GOOS == "windows" {
		if len(path) >= 4 && path[0] == '/' && path[2] == ':' && path[3] == '/' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
		if strings.HasPrefix(path, `\\`) {
			return "", fmt.Errorf("remote file shares and device paths are unavailable")
		}
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file URI requires an absolute local path")
	}
	return path, nil
}

// AtomicWriteFile writes data to a file atomically using a temp file and rename.
// If the target file already exists, its permissions are preserved; otherwise perm is used.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Preserve existing file permissions when overwriting.
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ob-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}
