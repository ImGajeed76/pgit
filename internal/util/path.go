package util

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const (
	PgitDir    = ".pgit"
	ConfigFile = "config.toml"
	IndexFile  = "index"
	HeadFile   = "HEAD"
)

// FindRepoRoot walks up from the current directory to find .pgit directory.
// Returns the repository root path or error if not found.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return FindRepoRootFrom(dir)
}

// FindRepoRootFrom walks up from the given directory to find .pgit directory.
func FindRepoRootFrom(start string) (string, error) {
	dir := start
	for {
		pgitPath := filepath.Join(dir, PgitDir)
		if info, err := os.Stat(pgitPath); err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", ErrNotARepository
		}
		dir = parent
	}
}

// PgitPath returns the path to the .pgit directory.
func PgitPath(repoRoot string) string {
	return filepath.Join(repoRoot, PgitDir)
}

// ConfigPath returns the path to the config file.
func ConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, PgitDir, ConfigFile)
}

// IndexPath returns the path to the index file.
func IndexPath(repoRoot string) string {
	return filepath.Join(repoRoot, PgitDir, IndexFile)
}

// HeadPath returns the path to the HEAD file.
func HeadPath(repoRoot string) string {
	return filepath.Join(repoRoot, PgitDir, HeadFile)
}

// RelativePath converts an absolute path to a path relative to the repo root.
func RelativePath(repoRoot, absPath string) (string, error) {
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", err
	}
	// Normalize to forward slashes for consistency
	return filepath.ToSlash(rel), nil
}

// AbsolutePath converts a relative path to an absolute path.
func AbsolutePath(repoRoot, relPath string) string {
	// Convert from forward slashes
	relPath = filepath.FromSlash(relPath)
	return filepath.Join(repoRoot, relPath)
}

// IsInsideRepo checks if a path is inside the repository (not in .pgit).
func IsInsideRepo(repoRoot, path string) bool {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return false
	}
	// Check if path is outside repo or inside .pgit
	if strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, PgitDir) {
		return false
	}
	return true
}

// HashPath generates a short hash of a path (for database naming).
func HashPath(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:8]) // First 16 hex chars
}

// IsBinaryFile checks if a file is binary by looking for NUL bytes in the first 8KB.
func IsBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false, err
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}

// FileMode returns the Unix file mode as an integer.
func FileMode(path string) (int, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return int(info.Mode().Perm()) | int(info.Mode()&os.ModeType), nil
}

// IsSymlink checks if a path is a symbolic link.
func IsSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// ReadSymlink returns the target of a symbolic link.
func ReadSymlink(path string) (string, error) {
	return os.Readlink(path)
}
