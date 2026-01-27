package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
)

// HashBytes computes SHA256 hash of bytes and returns hex string.
func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// HashFile computes SHA256 hash of a file and returns hex string.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TreeEntry represents a file in the tree for hashing.
type TreeEntry struct {
	Mode        int
	Path        string
	ContentHash string
}

// ComputeTreeHash computes the tree hash from a list of files.
// Files must have non-empty ContentHash (deleted files are excluded).
// The hash is computed as SHA256 of sorted entries in format:
// "{mode} {path}\0{content_hash}"
func ComputeTreeHash(entries []TreeEntry) string {
	// Sort by path
	sorted := make([]TreeEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	h := sha256.New()
	for _, e := range sorted {
		// Format: "{mode} {path}\0{content_hash}"
		entry := []byte(fmt.Sprintf("%d %s\x00%s", e.Mode, e.Path, e.ContentHash))
		h.Write(entry)
	}
	return hex.EncodeToString(h.Sum(nil))
}
