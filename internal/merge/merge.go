// Package merge implements three-way merge for text files.
//
// A three-way merge uses three versions of a file:
//   - Base: the common ancestor version (what both sides started from)
//   - Local: the version with our changes
//   - Remote: the version with their changes
//
// The algorithm computes line-level diffs from base→local and base→remote,
// then walks through both diffs simultaneously to classify each region:
//   - Neither side changed → keep base lines
//   - Only one side changed → take that side's changes (auto-merge)
//   - Both sides changed identically → take either (they agree)
//   - Both sides changed differently → conflict (insert markers around just those lines)
package merge

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Result holds the outcome of a three-way merge.
type Result struct {
	// Content is the merged file content. If there are conflicts,
	// the conflicting regions are wrapped in conflict markers.
	Content []byte

	// HasConflicts is true if any regions could not be auto-merged.
	HasConflicts bool

	// Conflicts lists each conflicting region in the output.
	Conflicts []Conflict

	// AutoResolved is the number of regions where only one side changed
	// and the change was applied automatically.
	AutoResolved int
}

// Conflict describes a single conflicting region in the merged output.
type Conflict struct {
	// OutputStartLine is the 1-based line number in the merged output
	// where the <<<<<<< marker appears.
	OutputStartLine int

	// LocalLines are the local side's lines for this conflict.
	LocalLines []string

	// RemoteLines are the remote side's lines for this conflict.
	RemoteLines []string
}

// conflictMarkerLocal is the marker that begins the local side of a conflict.
const conflictMarkerLocal = "<<<<<<< LOCAL"

// conflictMarkerSeparator separates local and remote sides.
const conflictMarkerSeparator = "======="

// conflictMarkerRemote is the marker that ends a conflict region.
// The remote name is appended in parentheses when writing.
const conflictMarkerRemote = ">>>>>>> REMOTE"

// ThreeWay performs a three-way merge of base, local, and remote content.
// remoteName is used to annotate the conflict marker (e.g., "origin").
//
// The merge works at line granularity:
//  1. Compute edit scripts base→local and base→remote
//  2. Convert each edit script into a sequence of "edit regions" — contiguous
//     groups of lines that were changed together
//  3. Walk both edit region lists simultaneously against the base, detecting
//     overlaps and classifying each region
//  4. Produce merged output with inline conflict markers only where needed
func ThreeWay(base, local, remote []byte, remoteName string) *Result {
	baseStr := string(base)
	localStr := string(local)
	remoteStr := string(remote)

	baseLines := splitLines(baseStr)
	localLines := splitLines(localStr)
	remoteLines := splitLines(remoteStr)

	// Compute line-level diffs from base to each side
	localEdits := computeEditRegions(baseLines, localLines)
	remoteEdits := computeEditRegions(baseLines, remoteLines)

	// Merge the two edit region lists against the base
	result := mergeRegions(baseLines, localLines, remoteLines, localEdits, remoteEdits, remoteName)

	// Determine trailing newline for the merged output.
	// If there are no conflicts, the merged output should preserve the trailing
	// newline convention of whichever side(s) contributed changes.
	// If no side changed, preserve base's convention.
	hasTrailingNL := hasTrailingNewline(baseStr)
	if len(localEdits) > 0 {
		hasTrailingNL = hasTrailingNewline(localStr)
	}
	if len(remoteEdits) > 0 {
		hasTrailingNL = hasTrailingNewline(remoteStr)
	}
	// If both changed, conflicts already include their content;
	// for the non-conflicting case, either side's convention works.

	if result.HasConflicts {
		// With conflict markers, always end with newline for clean display
		hasTrailingNL = true
	}

	if hasTrailingNL && len(result.Content) > 0 && result.Content[len(result.Content)-1] != '\n' {
		result.Content = append(result.Content, '\n')
	}

	return result
}

// splitLines splits text into lines. The trailing newline (if any) is stripped
// so that "foo\nbar\n" and "foo\nbar" both produce ["foo", "bar"].
// This keeps line indices consistent with countLines.
// An empty string returns an empty slice.
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// hasTrailingNewline returns true if s ends with a newline character.
func hasTrailingNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}

// editRegion represents a contiguous region of change between base and a side.
// It covers base lines [baseStart, baseEnd) which were replaced by
// the lines [sideStart, sideEnd) in the modified version.
type editRegion struct {
	baseStart int // inclusive index into base lines
	baseEnd   int // exclusive index into base lines
	sideStart int // inclusive index into the modified side's lines
	sideEnd   int // exclusive index into the modified side's lines
}

// computeEditRegions computes the diff between base and side, then groups
// consecutive insertions/deletions into contiguous edit regions.
//
// This is the core conversion: from a flat list of diff operations to
// structured regions that we can compare and overlap-test.
func computeEditRegions(base, side []string) []editRegion {
	dmp := diffmatchpatch.New()

	// Reconstruct text from lines (with newline separators) so the diff
	// library can map each line to a unique rune for line-level comparison.
	baseText := strings.Join(base, "\n")
	sideText := strings.Join(side, "\n")

	// If both are empty, no edits
	if baseText == sideText {
		return nil
	}

	chars1, chars2, lineArray := dmp.DiffLinesToRunes(baseText, sideText)
	diffs := dmp.DiffMainRunes(chars1, chars2, false)
	diffs = dmp.DiffCleanupSemantic(diffs)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	// Convert diff operations into edit regions.
	// Walk through diffs tracking position in base and side.
	var regions []editRegion
	basePos := 0
	sidePos := 0

	i := 0
	for i < len(diffs) {
		d := diffs[i]

		if d.Type == diffmatchpatch.DiffEqual {
			// Equal region — advance both cursors
			n := countLines(d.Text)
			basePos += n
			sidePos += n
			i++
			continue
		}

		// Start of a changed region. Collect adjacent Delete+Insert pairs.
		regionBaseStart := basePos
		regionSideStart := sidePos

		for i < len(diffs) && diffs[i].Type != diffmatchpatch.DiffEqual {
			switch diffs[i].Type {
			case diffmatchpatch.DiffDelete:
				basePos += countLines(diffs[i].Text)
			case diffmatchpatch.DiffInsert:
				sidePos += countLines(diffs[i].Text)
			}
			i++
		}

		regions = append(regions, editRegion{
			baseStart: regionBaseStart,
			baseEnd:   basePos,
			sideStart: regionSideStart,
			sideEnd:   sidePos,
		})
	}

	return regions
}

// countLines counts the number of lines in a diff text chunk.
// go-diff's DiffCharsToLines produces text where each "char" was originally
// a full line (with its trailing newline). So "foo\nbar\n" = 2 lines.
// A chunk without trailing newline like "foo" is also 1 line.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if text[len(text)-1] != '\n' {
		n++
	}
	return n
}

// mergeRegions walks both edit region lists against the base and produces
// the merged output.
func mergeRegions(
	baseLines, localLines, remoteLines []string,
	localEdits, remoteEdits []editRegion,
	remoteName string,
) *Result {
	var output []string
	var conflicts []Conflict
	autoResolved := 0

	basePos := 0
	li := 0 // index into localEdits
	ri := 0 // index into remoteEdits

	remoteMarkerEnd := conflictMarkerRemote
	if remoteName != "" {
		remoteMarkerEnd = conflictMarkerRemote + " (" + remoteName + ")"
	}

	for li < len(localEdits) || ri < len(remoteEdits) {
		var le *editRegion
		var re *editRegion

		if li < len(localEdits) {
			le = &localEdits[li]
		}
		if ri < len(remoteEdits) {
			re = &remoteEdits[ri]
		}

		// Determine whether the two edits overlap.
		//
		// Two edits overlap when their base ranges intersect. A subtle case:
		// pure insertions have baseStart == baseEnd (zero-width). Two
		// insertions at the same point both "touch" that point and must be
		// treated as overlapping, even though neither deletes any base lines.
		//
		// "Entirely before" means the edit's base range ends strictly before
		// the other starts. For a nonzero-width edit ending at position P and
		// another starting at P, they are adjacent but not overlapping (one
		// replaced lines up to P, the other starts replacing at P).
		// For a zero-width insertion at P, baseEnd == baseStart == P, so
		// "strictly before" (baseEnd < otherStart) is false when the other
		// also starts at P — correctly detecting the overlap.
		if le != nil && re != nil {
			if entirelyBefore(le, re) {
				// Local edit comes entirely before remote — no overlap.
				output = append(output, baseLines[basePos:le.baseStart]...)
				output = append(output, localLines[le.sideStart:le.sideEnd]...)
				basePos = le.baseEnd
				li++
				autoResolved++
				continue
			}

			if entirelyBefore(re, le) {
				// Remote edit comes entirely before local — no overlap.
				output = append(output, baseLines[basePos:re.baseStart]...)
				output = append(output, remoteLines[re.sideStart:re.sideEnd]...)
				basePos = re.baseEnd
				ri++
				autoResolved++
				continue
			}

			// Overlapping regions — potential conflict.
			// Determine the full extent of the overlap.
			overlapBaseStart := minInt(le.baseStart, re.baseStart)
			overlapBaseEnd := maxInt(le.baseEnd, re.baseEnd)

			// Advance past any additional edits on either side that fall
			// within this overlapping range. Edits can cascade: if local
			// edit A overlaps remote edit B, and remote edit B overlaps
			// local edit C, all three are part of the same conflict block.
			for {
				expanded := false
				for li < len(localEdits) && localEdits[li].baseStart <= overlapBaseEnd {
					if localEdits[li].baseEnd > overlapBaseEnd {
						overlapBaseEnd = localEdits[li].baseEnd
						expanded = true
					}
					li++
				}
				for ri < len(remoteEdits) && remoteEdits[ri].baseStart <= overlapBaseEnd {
					if remoteEdits[ri].baseEnd > overlapBaseEnd {
						overlapBaseEnd = remoteEdits[ri].baseEnd
						expanded = true
					}
					ri++
				}
				if !expanded {
					break
				}
			}

			// Collect local and remote lines for the overlapping region.
			// We need to reconstruct what each side looks like for this
			// base range by applying their edits.
			localOverlap := reconstructSide(baseLines, localLines, localEdits, overlapBaseStart, overlapBaseEnd, li)
			remoteOverlap := reconstructSide(baseLines, remoteLines, remoteEdits, overlapBaseStart, overlapBaseEnd, ri)

			// Emit base lines before the overlap
			output = append(output, baseLines[basePos:overlapBaseStart]...)

			// Check if both sides made identical changes
			if linesEqual(localOverlap, remoteOverlap) {
				// Both sides agree — take either
				output = append(output, localOverlap...)
				autoResolved++
			} else {
				// True conflict — emit markers around just the conflicting lines
				conflictStartLine := len(output) + 1 // 1-based

				conflict := Conflict{
					OutputStartLine: conflictStartLine,
					LocalLines:      localOverlap,
					RemoteLines:     remoteOverlap,
				}
				conflicts = append(conflicts, conflict)

				output = append(output, conflictMarkerLocal)
				output = append(output, localOverlap...)
				output = append(output, conflictMarkerSeparator)
				output = append(output, remoteOverlap...)
				output = append(output, remoteMarkerEnd)
			}

			basePos = overlapBaseEnd
			continue
		}

		// Only one side has remaining edits
		if le != nil {
			output = append(output, baseLines[basePos:le.baseStart]...)
			output = append(output, localLines[le.sideStart:le.sideEnd]...)
			basePos = le.baseEnd
			li++
			autoResolved++
			continue
		}

		if re != nil {
			output = append(output, baseLines[basePos:re.baseStart]...)
			output = append(output, remoteLines[re.sideStart:re.sideEnd]...)
			basePos = re.baseEnd
			ri++
			autoResolved++
			continue
		}
	}

	// Emit remaining base lines after all edits
	if basePos < len(baseLines) {
		output = append(output, baseLines[basePos:]...)
	}

	// Reconstruct the final content from lines
	content := strings.Join(output, "\n")

	return &Result{
		Content:      []byte(content),
		HasConflicts: len(conflicts) > 0,
		Conflicts:    conflicts,
		AutoResolved: autoResolved,
	}
}

// entirelyBefore returns true if edit a ends strictly before edit b starts,
// meaning they don't overlap. Handles zero-width insertions correctly:
// two insertions at the same point DO overlap (returns false).
func entirelyBefore(a, b *editRegion) bool {
	if a.baseEnd < b.baseStart {
		return true // Clearly before
	}
	if a.baseEnd == b.baseStart {
		// Adjacent. Only "before" if a has nonzero width (it consumed
		// base lines up to this point, and b starts fresh here).
		// If a is zero-width (pure insertion) at the same point as b,
		// they overlap.
		return a.baseStart < a.baseEnd
	}
	return false
}

// reconstructSide rebuilds what a given side looks like for the base range
// [overlapBaseStart, overlapBaseEnd]. It applies any of that side's edits
// that fall within the range, and fills in base lines for any gaps.
//
// editLimit is the index in the side's edit list up to which we've already
// consumed edits during the cascade. We scan from 0 to editLimit.
func reconstructSide(
	baseLines, sideLines []string,
	edits []editRegion,
	overlapBaseStart, overlapBaseEnd int,
	editLimit int,
) []string {
	var result []string
	pos := overlapBaseStart

	for i := 0; i < editLimit; i++ {
		e := edits[i]

		// Skip edits entirely before our range.
		// For zero-width insertions: baseStart == baseEnd == P. If P < overlapBaseStart,
		// the insertion is before our range. If P == overlapBaseStart, it's at the start.
		if e.baseEnd < overlapBaseStart {
			continue
		}
		if e.baseStart == e.baseEnd && e.baseStart < overlapBaseStart {
			continue
		}

		// Stop at edits entirely after our range
		if e.baseStart > overlapBaseEnd {
			break
		}

		// Emit base lines before this edit (within our range)
		editStart := maxInt(e.baseStart, overlapBaseStart)
		if pos < editStart {
			result = append(result, baseLines[pos:editStart]...)
		}

		// Emit this edit's replacement lines
		result = append(result, sideLines[e.sideStart:e.sideEnd]...)

		if e.baseEnd > pos {
			pos = e.baseEnd
		}
	}

	// Emit remaining base lines in the range
	if pos < overlapBaseEnd {
		result = append(result, baseLines[pos:overlapBaseEnd]...)
	}

	return result
}

// linesEqual compares two string slices for equality.
func linesEqual(a, b []string) bool {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
