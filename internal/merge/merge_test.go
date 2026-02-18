package merge

import (
	"strings"
	"testing"
)

// helper to build file content from lines
func lines(ls ...string) []byte {
	return []byte(strings.Join(ls, "\n") + "\n")
}

// helper to build file content from lines without trailing newline
func linesNoTrail(ls ...string) []byte {
	return []byte(strings.Join(ls, "\n"))
}

func TestThreeWay_NoChanges(t *testing.T) {
	base := lines("line1", "line2", "line3")
	result := ThreeWay(base, base, base, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(base) {
		t.Fatalf("content changed:\n  got:  %q\n  want: %q", string(result.Content), string(base))
	}
}

func TestThreeWay_OnlyLocalChanged(t *testing.T) {
	base := lines("line1", "line2", "line3")
	local := lines("line1", "MODIFIED", "line3")
	remote := lines("line1", "line2", "line3") // unchanged

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(local) {
		t.Fatalf("expected local version:\n  got:  %q\n  want: %q", string(result.Content), string(local))
	}
	if result.AutoResolved != 1 {
		t.Fatalf("expected 1 auto-resolved, got %d", result.AutoResolved)
	}
}

func TestThreeWay_OnlyRemoteChanged(t *testing.T) {
	base := lines("line1", "line2", "line3")
	local := lines("line1", "line2", "line3")   // unchanged
	remote := lines("line1", "REMOTE", "line3") // changed

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(remote) {
		t.Fatalf("expected remote version:\n  got:  %q\n  want: %q", string(result.Content), string(remote))
	}
	if result.AutoResolved != 1 {
		t.Fatalf("expected 1 auto-resolved, got %d", result.AutoResolved)
	}
}

func TestThreeWay_BothChangedDifferentRegions(t *testing.T) {
	base := lines("aaa", "bbb", "ccc", "ddd", "eee")
	local := lines("LOCAL", "bbb", "ccc", "ddd", "eee")   // changed line 1
	remote := lines("aaa", "bbb", "ccc", "ddd", "REMOTE") // changed line 5

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — changes in different regions")
	}

	expected := lines("LOCAL", "bbb", "ccc", "ddd", "REMOTE")
	if string(result.Content) != string(expected) {
		t.Fatalf("expected auto-merged:\n  got:  %q\n  want: %q", string(result.Content), string(expected))
	}
	if result.AutoResolved != 2 {
		t.Fatalf("expected 2 auto-resolved, got %d", result.AutoResolved)
	}
}

func TestThreeWay_BothChangedSameLine_Conflict(t *testing.T) {
	base := lines("line1", "line2", "line3")
	local := lines("line1", "LOCAL EDIT", "line3")
	remote := lines("line1", "REMOTE EDIT", "line3")

	result := ThreeWay(base, local, remote, "origin")

	if !result.HasConflicts {
		t.Fatal("expected conflict")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	content := string(result.Content)
	if !strings.Contains(content, "<<<<<<< LOCAL") {
		t.Fatal("missing local marker")
	}
	if !strings.Contains(content, "=======") {
		t.Fatal("missing separator")
	}
	if !strings.Contains(content, ">>>>>>> REMOTE (origin)") {
		t.Fatal("missing remote marker")
	}
	if !strings.Contains(content, "LOCAL EDIT") {
		t.Fatal("missing local content in conflict")
	}
	if !strings.Contains(content, "REMOTE EDIT") {
		t.Fatal("missing remote content in conflict")
	}
	// Non-conflicting lines should still be present
	if !strings.Contains(content, "line1") {
		t.Fatal("missing unchanged line1")
	}
	if !strings.Contains(content, "line3") {
		t.Fatal("missing unchanged line3")
	}
}

func TestThreeWay_BothChangedIdentically(t *testing.T) {
	base := lines("line1", "line2", "line3")
	local := lines("line1", "SAME EDIT", "line3")
	remote := lines("line1", "SAME EDIT", "line3")

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — both sides made the same change")
	}
	expected := lines("line1", "SAME EDIT", "line3")
	if string(result.Content) != string(expected) {
		t.Fatalf("wrong content:\n  got:  %q\n  want: %q", string(result.Content), string(expected))
	}
}

func TestThreeWay_LocalAddsLines(t *testing.T) {
	base := lines("line1", "line3")
	local := lines("line1", "NEW LINE", "line3") // added in middle
	remote := lines("line1", "line3")            // unchanged

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(local) {
		t.Fatalf("expected local version:\n  got:  %q\n  want: %q", string(result.Content), string(local))
	}
}

func TestThreeWay_RemoteDeletesLines(t *testing.T) {
	base := lines("line1", "TO DELETE", "line3")
	local := lines("line1", "TO DELETE", "line3") // unchanged
	remote := lines("line1", "line3")             // deleted middle line

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(remote) {
		t.Fatalf("expected remote version:\n  got:  %q\n  want: %q", string(result.Content), string(remote))
	}
}

func TestThreeWay_ConflictOnlyAffectsConflictingRegion(t *testing.T) {
	// Large file where conflict is only in the middle.
	// Top and bottom should be clean.
	base := lines(
		"top1", "top2", "top3",
		"middle",
		"bot1", "bot2", "bot3",
	)
	local := lines(
		"top1", "top2", "top3",
		"LOCAL MIDDLE",
		"bot1", "bot2", "bot3",
	)
	remote := lines(
		"top1", "top2", "top3",
		"REMOTE MIDDLE",
		"bot1", "bot2", "bot3",
	)

	result := ThreeWay(base, local, remote, "origin")

	if !result.HasConflicts {
		t.Fatal("expected conflict")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	content := string(result.Content)
	resultLines := strings.Split(content, "\n")

	// Top lines should be clean
	if resultLines[0] != "top1" || resultLines[1] != "top2" || resultLines[2] != "top3" {
		t.Fatalf("top lines corrupted: %v", resultLines[:3])
	}

	// Find the conflict markers
	markerIdx := -1
	for i, l := range resultLines {
		if l == "<<<<<<< LOCAL" {
			markerIdx = i
			break
		}
	}
	if markerIdx == -1 {
		t.Fatal("no conflict marker found")
	}
	if markerIdx != 3 {
		t.Fatalf("conflict marker at wrong position: %d (expected 3)", markerIdx)
	}

	// After the conflict, bottom lines should be clean
	endMarkerIdx := -1
	for i, l := range resultLines {
		if strings.HasPrefix(l, ">>>>>>> REMOTE") {
			endMarkerIdx = i
			break
		}
	}
	if endMarkerIdx == -1 {
		t.Fatal("no end conflict marker")
	}

	// Lines after end marker should be bot1, bot2, bot3
	remaining := resultLines[endMarkerIdx+1:]
	if len(remaining) < 3 || remaining[0] != "bot1" || remaining[1] != "bot2" || remaining[2] != "bot3" {
		t.Fatalf("bottom lines corrupted: %v", remaining)
	}
}

func TestThreeWay_MultipleNonOverlappingEdits(t *testing.T) {
	base := lines(
		"header",
		"section-a",
		"divider",
		"section-b",
		"footer",
	)
	local := lines(
		"header",
		"LOCAL-A", // changed section-a
		"divider",
		"section-b",
		"footer",
	)
	remote := lines(
		"header",
		"section-a",
		"divider",
		"REMOTE-B", // changed section-b
		"footer",
	)

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — edits are in different regions")
	}

	expected := lines(
		"header",
		"LOCAL-A",
		"divider",
		"REMOTE-B",
		"footer",
	)
	if string(result.Content) != string(expected) {
		t.Fatalf("wrong merge result:\n  got:  %q\n  want: %q", string(result.Content), string(expected))
	}
}

func TestThreeWay_ConflictWithAdjacentCleanEdit(t *testing.T) {
	// Local changes line 2, remote changes lines 2 AND 4.
	// Line 2 conflicts, line 4 should be auto-merged from remote.
	base := lines("a", "b", "c", "d", "e")
	local := lines("a", "LOCAL-B", "c", "d", "e")
	remote := lines("a", "REMOTE-B", "c", "REMOTE-D", "e")

	result := ThreeWay(base, local, remote, "origin")

	if !result.HasConflicts {
		t.Fatal("expected conflict on line 2")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	content := string(result.Content)
	// The remote-only change to line 4 should be applied
	if !strings.Contains(content, "REMOTE-D") {
		t.Fatal("remote-only edit to line 4 should be auto-merged")
	}
}

func TestThreeWay_EmptyBase(t *testing.T) {
	base := []byte{}
	local := lines("new local content")
	remote := lines("new remote content")

	result := ThreeWay(base, local, remote, "origin")

	if !result.HasConflicts {
		t.Fatal("expected conflict — both added content to empty base")
	}
}

func TestThreeWay_EmptyBase_OnlyOneAdds(t *testing.T) {
	base := []byte{}
	local := lines("new content")
	remote := []byte{} // unchanged

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — only local added")
	}
	if string(result.Content) != string(local) {
		t.Fatalf("wrong content:\n  got:  %q\n  want: %q", string(result.Content), string(local))
	}
}

func TestThreeWay_LocalDeletesAll(t *testing.T) {
	base := lines("line1", "line2", "line3")
	local := []byte{}
	remote := lines("line1", "line2", "line3") // unchanged

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — only local deleted")
	}
	if string(result.Content) != string(local) {
		t.Fatalf("expected empty content, got %q", string(result.Content))
	}
}

func TestThreeWay_MultiLineEdit(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	local := lines("a", "X", "Y", "Z", "e")  // replaced b,c,d with X,Y,Z
	remote := lines("a", "b", "c", "d", "e") // unchanged

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	expected := lines("a", "X", "Y", "Z", "e")
	if string(result.Content) != string(expected) {
		t.Fatalf("wrong content:\n  got:  %q\n  want: %q", string(result.Content), string(expected))
	}
}

func TestThreeWay_RealWorldReadme(t *testing.T) {
	// Simulates the pgit test scenario: both users edit the README header
	base := lines(
		"# Tokio",
		"",
		"A runtime for writing reliable, asynchronous, and slim applications with",
		"the Rust programming language.",
	)
	local := lines(
		"# Tokio",
		"",
		"Edited by User B.",
		"",
		"A runtime for writing reliable, asynchronous, and slim applications with",
		"the Rust programming language.",
	)
	remote := lines(
		"# Tokio",
		"",
		"Edited by User A.",
		"",
		"A runtime for writing reliable, asynchronous, and slim applications with",
		"the Rust programming language.",
	)

	result := ThreeWay(base, local, remote, "origin")

	// Both inserted text after line 2 — this is an overlapping edit
	if !result.HasConflicts {
		t.Fatal("expected conflict — both inserted at the same location")
	}

	content := string(result.Content)
	if !strings.Contains(content, "<<<<<<< LOCAL") {
		t.Fatal("missing conflict markers")
	}
	if !strings.Contains(content, "Edited by User B.") {
		t.Fatal("missing local edit")
	}
	if !strings.Contains(content, "Edited by User A.") {
		t.Fatal("missing remote edit")
	}
	// The title should be clean
	if !strings.HasPrefix(content, "# Tokio\n") {
		t.Fatalf("title should be clean, got: %s", content[:20])
	}
}

func TestThreeWay_RealWorldDifferentSections(t *testing.T) {
	// User A edits the top, User B edits the bottom — should auto-merge
	base := lines(
		"# Project",
		"",
		"Description here.",
		"",
		"## Installation",
		"",
		"Run: npm install",
		"",
		"## Usage",
		"",
		"Run: npm start",
	)
	local := lines(
		"# Project",
		"",
		"Description here.",
		"",
		"## Installation",
		"",
		"Run: npm install",
		"",
		"## Usage",
		"",
		"Run: npm run dev", // User B changed this line
	)
	remote := lines(
		"# My Project", // User A changed the title
		"",
		"Description here.",
		"",
		"## Installation",
		"",
		"Run: npm install",
		"",
		"## Usage",
		"",
		"Run: npm start",
	)

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — edits are in different sections")
	}

	expected := lines(
		"# My Project",
		"",
		"Description here.",
		"",
		"## Installation",
		"",
		"Run: npm install",
		"",
		"## Usage",
		"",
		"Run: npm run dev",
	)
	if string(result.Content) != string(expected) {
		t.Fatalf("wrong auto-merge result:\n  got:\n%s\n  want:\n%s", string(result.Content), string(expected))
	}
}

func TestThreeWay_NoTrailingNewline(t *testing.T) {
	base := linesNoTrail("line1", "line2")
	local := linesNoTrail("line1", "LOCAL")
	remote := linesNoTrail("line1", "line2")

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if string(result.Content) != string(local) {
		t.Fatalf("wrong content:\n  got:  %q\n  want: %q", string(result.Content), string(local))
	}
}

func TestThreeWay_BothAddLinesAtEnd(t *testing.T) {
	base := lines("line1", "line2")
	local := lines("line1", "line2", "local-added")
	remote := lines("line1", "line2", "remote-added")

	result := ThreeWay(base, local, remote, "origin")

	// Both added at the same position (end of file) — conflict
	if !result.HasConflicts {
		t.Fatal("expected conflict — both added lines at end")
	}

	content := string(result.Content)
	if !strings.Contains(content, "local-added") {
		t.Fatal("missing local addition")
	}
	if !strings.Contains(content, "remote-added") {
		t.Fatal("missing remote addition")
	}
}

func TestThreeWay_BothAddIdenticalLinesAtEnd(t *testing.T) {
	base := lines("line1", "line2")
	local := lines("line1", "line2", "same-line")
	remote := lines("line1", "line2", "same-line")

	result := ThreeWay(base, local, remote, "origin")

	if result.HasConflicts {
		t.Fatal("expected no conflicts — both added the same content")
	}
	expected := lines("line1", "line2", "same-line")
	if string(result.Content) != string(expected) {
		t.Fatalf("wrong content:\n  got:  %q\n  want: %q", string(result.Content), string(expected))
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"foo\n", 1},
		{"foo\nbar\n", 2},
		{"foo", 1},
		{"foo\nbar", 2},
	}

	for _, tt := range tests {
		got := countLines(tt.input)
		if got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", []string{}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\nb", []string{"a", "b"}},
		{"single", []string{"single"}},
		{"single\n", []string{"single"}},
	}

	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitLines(%q): len=%d, want len=%d\n  got:  %v\n  want: %v",
				tt.input, len(got), len(tt.want), got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
