package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnorePatterns holds the patterns from .gitignore and .pgitignore files
type IgnorePatterns struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string
	negation bool // Pattern starts with !
	dirOnly  bool // Pattern ends with /
}

// LoadIgnorePatterns loads patterns from .gitignore and .pgitignore files
func LoadIgnorePatterns(repoRoot string) (*IgnorePatterns, error) {
	ip := &IgnorePatterns{}

	// Always ignore .pgit directory
	ip.patterns = append(ip.patterns, ignorePattern{pattern: ".pgit", dirOnly: true})

	// Load .gitignore
	gitignore := filepath.Join(repoRoot, ".gitignore")
	if err := ip.loadFile(gitignore); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Load .pgitignore (takes precedence)
	pgitignore := filepath.Join(repoRoot, ".pgitignore")
	if err := ip.loadFile(pgitignore); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return ip, nil
}

func (ip *IgnorePatterns) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		ip.addPattern(line)
	}
	return scanner.Err()
}

func (ip *IgnorePatterns) addPattern(line string) {
	// Trim whitespace
	line = strings.TrimSpace(line)

	// Skip empty lines and comments
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}

	p := ignorePattern{}

	// Check for negation
	if strings.HasPrefix(line, "!") {
		p.negation = true
		line = line[1:]
	}

	// Check for directory-only pattern
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	p.pattern = line
	ip.patterns = append(ip.patterns, p)
}

// IsIgnored checks if a path should be ignored
func (ip *IgnorePatterns) IsIgnored(path string, isDir bool) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)

	ignored := false
	for _, p := range ip.patterns {
		// Skip directory-only patterns for files
		if p.dirOnly && !isDir {
			continue
		}

		if ip.matches(p.pattern, path) {
			ignored = !p.negation
		}
	}
	return ignored
}

// matches checks if a pattern matches a path
// Supports basic gitignore patterns:
// - * matches anything except /
// - ** matches anything including /
// - ? matches any single character
// - [abc] matches any character in brackets
//
// Per gitignore spec:
// - If pattern has no slash, it matches basename at any level
// - If pattern has slash in middle/beginning, it's relative to .gitignore location (root)
// - Pattern starting with / is anchored to root
func (ip *IgnorePatterns) matches(pattern, path string) bool {
	// Handle patterns without /
	if !strings.Contains(pattern, "/") {
		// No slash: match against basename at any level
		basename := filepath.Base(path)
		return matchGlob(pattern, basename)
	}

	// Pattern contains / - it's relative to root (per gitignore spec)
	// "If there is a separator at the beginning or middle (or both) of the
	// pattern, then the pattern is relative to the directory level of the
	// particular .gitignore file itself."

	// Handle patterns starting with /
	pattern = strings.TrimPrefix(pattern, "/")

	// Match against full path from root
	return matchGlob(pattern, path)
}

// matchGlob performs glob matching with support for *, **, ?, and [...]
func matchGlob(pattern, name string) bool {
	// Handle ** separately
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, name)
	}

	// Use filepath.Match for simple patterns
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// matchDoublestar handles ** patterns
func matchDoublestar(pattern, name string) bool {
	// Split pattern by **
	parts := strings.Split(pattern, "**")
	if len(parts) == 1 {
		// No ** in pattern
		matched, _ := filepath.Match(pattern, name)
		return matched
	}

	// For now, simple implementation:
	// ** at start: match suffix
	// ** at end: match prefix
	// ** in middle: match prefix and suffix

	if pattern == "**" {
		return true
	}

	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		return matchGlob(suffix, name) || matchGlob(suffix, filepath.Base(name))
	}

	if strings.HasSuffix(pattern, "/**") {
		prefix := pattern[:len(pattern)-3]
		return strings.HasPrefix(name, prefix+"/") || name == prefix
	}

	// General case: try all possible splits
	prefix := parts[0]
	suffix := parts[1]

	if prefix != "" && !strings.HasPrefix(name, prefix) {
		return false
	}
	if suffix != "" && !strings.HasSuffix(name, suffix) {
		return false
	}

	return true
}

// ShouldInclude returns true if a path should be included (not ignored)
func (ip *IgnorePatterns) ShouldInclude(path string, isDir bool) bool {
	return !ip.IsIgnored(path, isDir)
}
