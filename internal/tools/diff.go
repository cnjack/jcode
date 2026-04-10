package tools

import (
	"fmt"
	"strings"
)

// generateUnifiedDiff produces a unified diff between original and modified content.
// The output uses the standard unified diff format with --- / +++ headers and @@ hunks.
func generateUnifiedDiff(original, modified, filename string) string {
	if original == modified {
		return ""
	}

	oldLines := splitLines(original)
	newLines := splitLines(modified)

	// Compute edit script using Myers-like O(ND) approach (simplified LCS).
	lcs := computeLCS(oldLines, newLines)

	// Build hunks from the LCS.
	hunks := buildHunks(oldLines, newLines, lcs, 3)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", filename))

	for _, h := range hunks {
		sb.WriteString(h)
	}

	return sb.String()
}

// splitLines splits text into lines, preserving the count correctly.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lcsEntry tracks an LCS match: (old index, new index).
type lcsEntry struct {
	oldIdx int
	newIdx int
}

// computeLCS computes the Longest Common Subsequence between two string slices
// using a standard DP approach. Returns matched pairs of (oldIndex, newIndex).
func computeLCS(a, b []string) []lcsEntry {
	m, n := len(a), len(b)

	// For very large files, bail out with a simpler approach.
	if int64(m)*int64(n) > 10_000_000 {
		return computeLCSGreedy(a, b)
	}

	// Standard DP table (space-optimised variant stores only two rows for the length,
	// but we need the actual subsequence, so use full table for moderate sizes).
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to find the LCS entries.
	var result []lcsEntry
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append(result, lcsEntry{i - 1, j - 1})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	// Reverse to get ascending order.
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

// computeLCSGreedy is a fallback for very large files.
// It uses a greedy forward matching approach which is O(n+m) but not optimal.
func computeLCSGreedy(a, b []string) []lcsEntry {
	var result []lcsEntry
	j := 0
	for i := 0; i < len(a) && j < len(b); i++ {
		for j < len(b) {
			if a[i] == b[j] {
				result = append(result, lcsEntry{i, j})
				j++
				break
			}
			j++
		}
	}
	return result
}

// buildHunks creates unified diff hunks from old lines, new lines, and the LCS.
// contextLines specifies how many unchanged lines to show around changes.
func buildHunks(oldLines, newLines []string, lcs []lcsEntry, contextLines int) []string {

	// Build a full diff sequence from the LCS.
	var diff []diffLine
	oi, ni := 0, 0
	for _, m := range lcs {
		// Lines deleted from old before this match.
		for oi < m.oldIdx {
			diff = append(diff, diffLine{'-', oldLines[oi], oi + 1, 0})
			oi++
		}
		// Lines added in new before this match.
		for ni < m.newIdx {
			diff = append(diff, diffLine{'+', newLines[ni], 0, ni + 1})
			ni++
		}
		// Matched line.
		diff = append(diff, diffLine{' ', oldLines[oi], oi + 1, ni + 1})
		oi++
		ni++
	}
	// Remaining lines after last LCS match.
	for oi < len(oldLines) {
		diff = append(diff, diffLine{'-', oldLines[oi], oi + 1, 0})
		oi++
	}
	for ni < len(newLines) {
		diff = append(diff, diffLine{'+', newLines[ni], 0, ni + 1})
		ni++
	}

	// Identify change regions and build hunks with context.
	type region struct {
		start, end int // indices into diff
	}

	var regions []region
	inChange := false
	start := 0
	for i, d := range diff {
		if d.op != ' ' {
			if !inChange {
				inChange = true
				start = i
			}
		} else {
			if inChange {
				regions = append(regions, region{start, i})
				inChange = false
			}
		}
	}
	if inChange {
		regions = append(regions, region{start, len(diff)})
	}

	if len(regions) == 0 {
		return nil
	}

	// Merge regions that are within contextLines*2 of each other, then format hunks.
	type hunkRange struct {
		start, end int
	}

	var merged []hunkRange
	cur := hunkRange{
		start: max(0, regions[0].start-contextLines),
		end:   min(len(diff), regions[0].end+contextLines),
	}

	for i := 1; i < len(regions); i++ {
		rStart := max(0, regions[i].start-contextLines)
		rEnd := min(len(diff), regions[i].end+contextLines)
		if rStart <= cur.end {
			cur.end = rEnd
		} else {
			merged = append(merged, cur)
			cur = hunkRange{rStart, rEnd}
		}
	}
	merged = append(merged, cur)

	// Format each hunk.
	var hunks []string
	for _, hr := range merged {
		var hunkLines []string
		oldStart, newStart := 0, 0
		oldCount, newCount := 0, 0
		first := true

		for i := hr.start; i < hr.end; i++ {
			d := diff[i]
			if first {
				switch d.op {
				case ' ':
					oldStart = d.oldN
					newStart = d.newN
				case '-':
					oldStart = d.oldN
					// Find the new line number from context.
					newStart = findNewLineAt(diff, i)
				case '+':
					newStart = d.newN
					oldStart = findOldLineAt(diff, i)
				}
				first = false
			}
			switch d.op {
			case ' ':
				hunkLines = append(hunkLines, " "+d.text)
				oldCount++
				newCount++
			case '-':
				hunkLines = append(hunkLines, "-"+d.text)
				oldCount++
			case '+':
				hunkLines = append(hunkLines, "+"+d.text)
				newCount++
			}
		}

		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}

		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		var sb strings.Builder
		sb.WriteString(header)
		for _, l := range hunkLines {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
		hunks = append(hunks, sb.String())
	}

	return hunks
}

// diffLine represents a single line in the diff output.
type diffLine struct {
	op   byte // ' ', '+', '-'
	text string
	oldN int // 1-based line number in old file (0 if not applicable)
	newN int // 1-based line number in new file (0 if not applicable)
}

// findNewLineAt looks backward and forward from position i in the diff to find
// the corresponding new-file line number.
func findNewLineAt(diff []diffLine, i int) int {
	for j := i - 1; j >= 0; j-- {
		if diff[j].newN > 0 {
			return diff[j].newN + 1
		}
	}
	for j := i + 1; j < len(diff); j++ {
		if diff[j].newN > 0 {
			return 1
		}
	}
	return 1
}

// findOldLineAt looks backward from position i to find the old-file line number.
func findOldLineAt(diff []diffLine, i int) int {
	for j := i - 1; j >= 0; j-- {
		if diff[j].oldN > 0 {
			return diff[j].oldN + 1
		}
	}
	return 1
}
