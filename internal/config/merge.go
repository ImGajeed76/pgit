package config

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/imgajeed76/pgit/internal/util"
)

// MergeState tracks the state of an in-progress merge/pull operation
type MergeState struct {
	// InProgress indicates a merge is in progress
	InProgress bool `json:"in_progress"`

	// RemoteName is the remote we're merging from
	RemoteName string `json:"remote_name"`

	// RemoteCommitID is the commit we're merging in
	RemoteCommitID string `json:"remote_commit_id"`

	// LocalCommitID is our commit before the merge
	LocalCommitID string `json:"local_commit_id"`

	// CommonAncestor is the common ancestor commit
	CommonAncestor string `json:"common_ancestor"`

	// ConflictedFiles lists files with merge conflicts
	ConflictedFiles []string `json:"conflicted_files"`
}

const MergeStateFile = "MERGE_STATE"

// MergeStatePath returns the path to the merge state file
func MergeStatePath(repoRoot string) string {
	return filepath.Join(repoRoot, util.PgitDir, MergeStateFile)
}

// LoadMergeState loads the merge state from disk
func LoadMergeState(repoRoot string) (*MergeState, error) {
	path := MergeStatePath(repoRoot)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &MergeState{InProgress: false}, nil
	}
	if err != nil {
		return nil, err
	}

	var state MergeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// Save writes the merge state to disk
func (m *MergeState) Save(repoRoot string) error {
	path := MergeStatePath(repoRoot)

	if !m.InProgress {
		// Remove the file if no merge in progress
		os.Remove(path)
		return nil
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Clear removes the merge state
func (m *MergeState) Clear(repoRoot string) error {
	m.InProgress = false
	m.ConflictedFiles = nil
	return os.Remove(MergeStatePath(repoRoot))
}

// HasConflicts returns true if there are unresolved conflicts
func (m *MergeState) HasConflicts() bool {
	return m.InProgress && len(m.ConflictedFiles) > 0
}

// AddConflict adds a file to the conflict list
func (m *MergeState) AddConflict(path string) {
	for _, p := range m.ConflictedFiles {
		if p == path {
			return // Already in list
		}
	}
	m.ConflictedFiles = append(m.ConflictedFiles, path)
}

// RemoveConflict removes a file from the conflict list
func (m *MergeState) RemoveConflict(path string) {
	var newList []string
	for _, p := range m.ConflictedFiles {
		if p != path {
			newList = append(newList, p)
		}
	}
	m.ConflictedFiles = newList
}

// IsConflicted checks if a file is in the conflict list
func (m *MergeState) IsConflicted(path string) bool {
	for _, p := range m.ConflictedFiles {
		if p == path {
			return true
		}
	}
	return false
}

// ConflictMarkers for merge conflicts
const (
	ConflictMarkerStart  = "<<<<<<< LOCAL"
	ConflictMarkerMiddle = "======="
	ConflictMarkerEnd    = ">>>>>>> REMOTE"
)

// CreateConflictedFile writes a file with conflict markers
func CreateConflictedFile(path string, localContent, remoteContent []byte, remoteName string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	// Write conflict markers
	w.WriteString(ConflictMarkerStart + "\n")
	w.Write(localContent)
	if len(localContent) > 0 && localContent[len(localContent)-1] != '\n' {
		w.WriteString("\n")
	}
	w.WriteString(ConflictMarkerMiddle + "\n")
	w.Write(remoteContent)
	if len(remoteContent) > 0 && remoteContent[len(remoteContent)-1] != '\n' {
		w.WriteString("\n")
	}
	w.WriteString(ConflictMarkerEnd + " (" + remoteName + ")\n")

	return w.Flush()
}

// HasConflictMarkers checks if a file contains conflict markers
func HasConflictMarkers(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == ConflictMarkerStart ||
			line == ConflictMarkerMiddle ||
			len(line) > len(ConflictMarkerEnd) && line[:len(ConflictMarkerEnd)] == ConflictMarkerEnd {
			return true, nil
		}
	}

	return false, scanner.Err()
}
