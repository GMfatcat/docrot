// Package fix applies the mechanical half of docrot's suggestions: the
// path rewrites the resolver marked as certain (a rename recorded in git
// history, a path that differs from the real one only by letter case). It
// plans edits from findings and applies them line by line, keeping the
// file's line endings and byte-order mark. Symbol renames and fuzzy
// did-you-mean suggestions are never applied.
package fix

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"docrot/internal/model"
)

// DataKey is the Finding.Data entry the resolver fills with the corrected
// text when a fix is mechanical.
const DataKey = "fix"

// Edit is one text replacement on one line of one document.
type Edit struct {
	File string `json:"file"` // repo-relative, forward slashes
	Line int    `json:"line"` // 1-based
	Col  int    `json:"col,omitempty"`
	Old  string `json:"old"`
	New  string `json:"new"`
	Rule string `json:"rule"`
}

// Plan collects the edits that findings allow: those carrying a fix text,
// a reference with its original spelling, and not frozen by the baseline.
// The result is sorted by file, line and column.
func Plan(findings []model.Finding) []Edit {
	var out []Edit
	seen := map[string]bool{}
	for _, f := range findings {
		if f.Baselined || f.Ref == nil || f.Ref.Text == "" || f.Loc.Line <= 0 {
			continue
		}
		text, ok := f.Data[DataKey].(string)
		if !ok || text == "" || text == f.Ref.Text {
			continue
		}
		e := Edit{File: f.Loc.File, Line: f.Loc.Line, Col: f.Loc.Col, Old: f.Ref.Text, New: text, Rule: f.Rule}
		key := fmt.Sprintf("%s:%d:%d:%s", e.File, e.Line, e.Col, e.Old)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return out
}

// Change is one line before and after its edits.
type Change struct {
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// FileResult is the outcome for one document.
type FileResult struct {
	File    string   `json:"file"`
	Applied []Edit   `json:"applied"`
	Skipped []Edit   `json:"skipped,omitempty"` // the old text was not on the line any more
	Changes []Change `json:"changes"`
}

// Apply performs the edits under root, grouped by file. With write false
// the files are left alone and the result shows what would change. Edits
// on one line are applied right to left so that earlier columns stay
// valid; an edit whose old text is not found on its line is skipped, not
// guessed. A file that cannot be read fails the whole call.
func Apply(root string, edits []Edit, write bool) ([]FileResult, error) {
	byFile := map[string][]Edit{}
	var order []string
	for _, e := range edits {
		if _, ok := byFile[e.File]; !ok {
			order = append(order, e.File)
		}
		byFile[e.File] = append(byFile[e.File], e)
	}
	sort.Strings(order)

	var out []FileResult
	for _, file := range order {
		res, err := applyFile(root, file, byFile[file], write)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

func applyFile(root, file string, edits []Edit, write bool) (FileResult, error) {
	res := FileResult{File: file}
	abs := filepath.Join(root, filepath.FromSlash(file))
	data, err := os.ReadFile(abs)
	if err != nil {
		return res, err
	}
	bom := bytes.HasPrefix(data, []byte("\xef\xbb\xbf"))
	if bom {
		data = data[3:]
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	trailingNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	// group by line, apply right to left within a line
	byLine := map[int][]Edit{}
	for _, e := range edits {
		byLine[e.Line] = append(byLine[e.Line], e)
	}
	var lineNos []int
	for n := range byLine {
		lineNos = append(lineNos, n)
	}
	sort.Ints(lineNos)
	changed := false
	for _, n := range lineNos {
		if n < 1 || n > len(lines) {
			res.Skipped = append(res.Skipped, byLine[n]...)
			continue
		}
		before := lines[n-1]
		after := before
		group := byLine[n]
		// locate every edit first, then apply from the right
		type located struct {
			Edit
			at int
		}
		var locs []located
		for _, e := range group {
			at := locate(after, e)
			if at < 0 {
				res.Skipped = append(res.Skipped, e)
				continue
			}
			locs = append(locs, located{e, at})
		}
		sort.Slice(locs, func(i, j int) bool { return locs[i].at > locs[j].at })
		for _, l := range locs {
			after = after[:l.at] + l.New + after[l.at+len(l.Old):]
			res.Applied = append(res.Applied, l.Edit)
		}
		if after != before {
			lines[n-1] = after
			res.Changes = append(res.Changes, Change{Line: n, Before: before, After: after})
			changed = true
		}
	}
	sort.Slice(res.Applied, func(i, j int) bool {
		if res.Applied[i].Line != res.Applied[j].Line {
			return res.Applied[i].Line < res.Applied[j].Line
		}
		return res.Applied[i].Col < res.Applied[j].Col
	})
	if !write || !changed {
		return res, nil
	}
	joined := strings.Join(lines, "\n")
	if trailingNewline {
		joined += "\n"
	}
	if crlf {
		joined = strings.ReplaceAll(joined, "\n", "\r\n")
	}
	var buf bytes.Buffer
	if bom {
		buf.WriteString("\xef\xbb\xbf")
	}
	buf.WriteString(joined)
	return res, os.WriteFile(abs, buf.Bytes(), 0o644)
}

// locate finds the byte offset of e.Old on line: at e.Col when the text
// starts there, else the first occurrence at or after e.Col (a link's
// column is where the bracket opens, not where the target starts), else
// the first occurrence anywhere. -1 when absent.
func locate(line string, e Edit) int {
	if e.Col > 0 && e.Col-1 <= len(line) {
		rest := line[e.Col-1:]
		if strings.HasPrefix(rest, e.Old) {
			return e.Col - 1
		}
		if i := strings.Index(rest, e.Old); i >= 0 {
			return e.Col - 1 + i
		}
	}
	return strings.Index(line, e.Old)
}
