package diff

import (
	"strings"

	godiff "github.com/sourcegraph/go-diff/diff"
)

// Parse parses a unified diff string into structured FileDiffs.
func Parse(raw string) ([]FileDiff, error) {
	fileDiffs, err := godiff.ParseMultiFileDiff([]byte(raw))
	if err != nil {
		return nil, err
	}

	var result []FileDiff
	for _, fd := range fileDiffs {
		f := FileDiff{
			OldName: cleanPath(fd.OrigName),
			NewName: cleanPath(fd.NewName),
		}

		for _, h := range fd.Hunks {
			hunk := parseHunk(h)
			hunk.Pairs = alignPairs(hunk.Lines)
			f.Hunks = append(f.Hunks, hunk)
		}

		result = append(result, f)
	}
	return result, nil
}

func cleanPath(p string) string {
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	return p
}

func parseHunk(h *godiff.Hunk) Hunk {
	hunk := Hunk{
		OldStart: int(h.OrigStartLine),
		OldCount: int(h.OrigLines),
		NewStart: int(h.NewStartLine),
		NewCount: int(h.NewLines),
		Section:  h.Section,
	}

	oldNum := int(h.OrigStartLine)
	newNum := int(h.NewStartLine)

	lines := strings.Split(string(h.Body), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case ' ':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Op:      OpEqual,
				Content: line[1:],
				OldNum:  oldNum,
				NewNum:  newNum,
			})
			oldNum++
			newNum++
		case '-':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Op:      OpDelete,
				Content: line[1:],
				OldNum:  oldNum,
			})
			oldNum++
		case '+':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Op:      OpInsert,
				Content: line[1:],
				NewNum:  newNum,
			})
			newNum++
		case '\\':
			// "\ No newline at end of file" — skip
		}
	}

	return hunk
}

// alignPairs creates side-by-side aligned pairs from diff lines.
// Adjacent delete+insert sequences are paired together.
func alignPairs(lines []DiffLine) []LinePair {
	var pairs []LinePair
	i := 0
	for i < len(lines) {
		line := lines[i]
		switch line.Op {
		case OpEqual:
			l := line
			r := line
			pairs = append(pairs, LinePair{Left: &l, Right: &r})
			i++
		case OpDelete:
			// Collect consecutive deletes
			var deletes []DiffLine
			for i < len(lines) && lines[i].Op == OpDelete {
				deletes = append(deletes, lines[i])
				i++
			}
			// Collect consecutive inserts
			var inserts []DiffLine
			for i < len(lines) && lines[i].Op == OpInsert {
				inserts = append(inserts, lines[i])
				i++
			}
			// Pair them up
			maxLen := len(deletes)
			if len(inserts) > maxLen {
				maxLen = len(inserts)
			}
			for j := 0; j < maxLen; j++ {
				pair := LinePair{}
				if j < len(deletes) {
					d := deletes[j]
					pair.Left = &d
				}
				if j < len(inserts) {
					ins := inserts[j]
					pair.Right = &ins
				}
				pairs = append(pairs, pair)
			}
		case OpInsert:
			ins := line
			pairs = append(pairs, LinePair{Right: &ins})
			i++
		}
	}
	return pairs
}
