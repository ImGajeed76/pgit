package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/zeebo/blake3"
)

// ContentHashSize is the size of BLAKE3 content hashes (16 bytes)
const ContentHashSize = 16

// HashBytes computes SHA256 hash of bytes and returns hex string.
// Deprecated: Use HashBytesBlake3 for new code.
func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// HashBytesBlake3 computes BLAKE3 hash of bytes and returns 16-byte slice.
// This is the primary hash function for content hashing in the new schema.
func HashBytesBlake3(data []byte) []byte {
	h := blake3.Sum256(data)
	// Truncate to 16 bytes (128 bits) - still extremely collision resistant
	result := make([]byte, ContentHashSize)
	copy(result, h[:ContentHashSize])
	return result
}

// HashBytesBlake3Hex computes BLAKE3 hash and returns 32-char hex string.
// Useful for debugging and display purposes.
func HashBytesBlake3Hex(data []byte) string {
	hash := HashBytesBlake3(data)
	return hex.EncodeToString(hash)
}

// HashFile computes SHA256 hash of a file and returns hex string.
// Deprecated: Use HashFileBlake3 for new code.
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

// HashFileBlake3 computes BLAKE3 hash of a file and returns 16-byte slice.
func HashFileBlake3(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	sum := h.Sum(nil)
	result := make([]byte, ContentHashSize)
	copy(result, sum[:ContentHashSize])
	return result, nil
}

// HashFileBlake3Hex computes BLAKE3 hash of a file and returns 32-char hex string.
func HashFileBlake3Hex(path string) (string, error) {
	hash, err := HashFileBlake3(path)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash), nil
}

// ContentHashEqual compares two content hashes for equality.
// Handles nil hashes (nil == nil is true, nil != non-nil is true).
func ContentHashEqual(a, b []byte) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ContentHashToHex converts a content hash to hex string for display.
// Returns empty string for nil hash.
func ContentHashToHex(hash []byte) string {
	if hash == nil {
		return ""
	}
	return hex.EncodeToString(hash)
}

// ContentHashFromHex parses a hex string to content hash.
// Returns nil for empty string.
func ContentHashFromHex(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return hex.DecodeString(s)
}

// DetectBinary returns true if content appears to be binary.
// Uses git's heuristic: a NUL byte in the first 8000 bytes means binary.
func DetectBinary(content []byte) bool {
	checkLen := len(content)
	if checkLen > 8000 {
		checkLen = 8000
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// TreeEntry represents a file in the tree for hashing.
type TreeEntry struct {
	Mode        int
	Path        string
	ContentHash []byte // 16 bytes BLAKE3 hash
}

// ComputeTreeHash computes the tree hash from a list of files using BLAKE3.
// Files must have non-empty ContentHash (deleted files are excluded).
// The hash is computed as BLAKE3 of sorted entries in format:
// "{mode} {path}\0{content_hash_bytes}"
func ComputeTreeHash(entries []TreeEntry) string {
	// Sort by path
	sorted := make([]TreeEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	h := blake3.New()
	for _, e := range sorted {
		// Format: "{mode} {path}\0" followed by raw hash bytes
		header := []byte(fmt.Sprintf("%d %s\x00", e.Mode, e.Path))
		_, _ = h.Write(header)
		_, _ = h.Write(e.ContentHash)
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])
}

// TreeEntryLegacy represents a file in the tree for hashing (legacy format).
// Used for backwards compatibility with existing commit tree hashes.
type TreeEntryLegacy struct {
	Mode        int
	Path        string
	ContentHash string // SHA256 hex string
}

// ComputeTreeHashLegacy computes tree hash using the old SHA256 format.
// Deprecated: Only use for reading existing commits.
func ComputeTreeHashLegacy(entries []TreeEntryLegacy) string {
	sorted := make([]TreeEntryLegacy, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	h := sha256.New()
	for _, e := range sorted {
		entry := []byte(fmt.Sprintf("%d %s\x00%s", e.Mode, e.Path, e.ContentHash))
		h.Write(entry)
	}
	return hex.EncodeToString(h.Sum(nil))
}
