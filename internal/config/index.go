package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imgajeed76/pgit/v4/internal/util"
)

// FileStatus represents the status of a file in the index
type FileStatus string

const (
	StatusAdded    FileStatus = "A" // New file
	StatusModified FileStatus = "M" // Modified file
	StatusDeleted  FileStatus = "D" // Deleted file
)

// IndexEntry represents a single entry in the staging index
type IndexEntry struct {
	Status FileStatus
	Path   string
}

// Index represents the staging area (.pgit/index)
type Index struct {
	Entries map[string]IndexEntry // path -> entry
}

// NewIndex creates a new empty index
func NewIndex() *Index {
	return &Index{
		Entries: make(map[string]IndexEntry),
	}
}

// LoadIndex reads the index file from the repository
func LoadIndex(repoRoot string) (*Index, error) {
	indexPath := util.IndexPath(repoRoot)

	idx := NewIndex()

	f, err := os.Open(indexPath)
	if os.IsNotExist(err) {
		// No index file yet, return empty index
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: "A path/to/file" or "M path/to/file" or "D path/to/file"
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}

		status := FileStatus(parts[0])
		path := parts[1]

		idx.Entries[path] = IndexEntry{
			Status: status,
			Path:   path,
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return idx, nil
}

// Save writes the index to the repository
func (idx *Index) Save(repoRoot string) error {
	indexPath := util.IndexPath(repoRoot)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(indexPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Sort entries by path for consistent output
	paths := make([]string, 0, len(idx.Entries))
	for path := range idx.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		entry := idx.Entries[path]
		fmt.Fprintf(f, "%s %s\n", entry.Status, entry.Path)
	}

	return nil
}

// Add stages a file for addition or modification
func (idx *Index) Add(path string, isNew bool) {
	status := StatusModified
	if isNew {
		status = StatusAdded
	}
	idx.Entries[path] = IndexEntry{
		Status: status,
		Path:   path,
	}
}

// Delete stages a file for deletion
func (idx *Index) Delete(path string) {
	idx.Entries[path] = IndexEntry{
		Status: StatusDeleted,
		Path:   path,
	}
}

// Remove unstages a file (removes from index)
func (idx *Index) Remove(path string) {
	delete(idx.Entries, path)
}

// Clear removes all entries from the index
func (idx *Index) Clear() {
	idx.Entries = make(map[string]IndexEntry)
}

// IsEmpty returns true if the index has no entries
func (idx *Index) IsEmpty() bool {
	return len(idx.Entries) == 0
}

// Get returns an entry by path
func (idx *Index) Get(path string) (IndexEntry, bool) {
	entry, ok := idx.Entries[path]
	return entry, ok
}

// List returns all entries sorted by path
func (idx *Index) List() []IndexEntry {
	paths := make([]string, 0, len(idx.Entries))
	for path := range idx.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	entries := make([]IndexEntry, len(paths))
	for i, path := range paths {
		entries[i] = idx.Entries[path]
	}
	return entries
}

// ListByStatus returns entries filtered by status
func (idx *Index) ListByStatus(status FileStatus) []IndexEntry {
	var entries []IndexEntry
	for _, entry := range idx.Entries {
		if entry.Status == status {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}
