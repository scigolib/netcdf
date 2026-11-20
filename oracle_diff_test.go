package netcdf_test

import (
	"fmt"
	"strings"
	"testing"
)

// firstDifference describes the first line where two texts differ, or the
// length difference after their common prefix. It returns "" when the texts
// are equal. It is the reporter of the oracle harness, kept in the untagged
// tests so the normal CI run proves that it can tell a difference at all.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	common := len(wantLines)
	if len(gotLines) < common {
		common = len(gotLines)
	}
	for i := 0; i < common; i++ {
		if wantLines[i] != gotLines[i] {
			return fmt.Sprintf("line %d:\n  want: %q\n  got:  %q", i+1, wantLines[i], gotLines[i])
		}
	}
	if len(wantLines) != len(gotLines) {
		return fmt.Sprintf("texts share %d lines, want %d lines, got %d", common, len(wantLines), len(gotLines))
	}
	return ""
}

func TestFirstDifference(t *testing.T) {
	tests := []struct {
		name     string
		want     string
		got      string
		contains []string // nil means the texts must compare equal
	}{
		{name: "equal", want: "a\nb\n", got: "a\nb\n"},
		{name: "first line", want: "a\nb\n", got: "X\nb\n", contains: []string{"line 1", `want: "a"`, `got:  "X"`}},
		{name: "middle line", want: "a\nb\nc\n", got: "a\nX\nc\n", contains: []string{"line 2", `want: "b"`, `got:  "X"`}},
		{name: "want longer", want: "a\nb\nc", got: "a\nb", contains: []string{"share 2 lines", "want 3 lines, got 2"}},
		{name: "got longer", want: "a\nb", got: "a\nb\nc", contains: []string{"share 2 lines", "want 2 lines, got 3"}},
		{name: "trailing newline", want: "a\nb\n", got: "a\nb", contains: []string{"share 2 lines", "want 3 lines, got 2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstDifference(tt.want, tt.got)
			if tt.contains == nil {
				if got != "" {
					t.Fatalf("firstDifference reported a difference for equal texts: %s", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("firstDifference missed a difference")
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("report %q does not contain %q", got, want)
				}
			}
		})
	}
}
