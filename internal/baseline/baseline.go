// Package baseline reads and writes the .docrot-baseline.json file that lets
// a repository freeze its existing findings and fail CI only on new ones.
//
// A baseline is a set of fingerprints (see model.Fingerprint), which
// deliberately exclude line and column numbers so that editing a document
// elsewhere does not invalidate every entry in it.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"docrot/internal/model"
)

// DefaultName is the file name docrot looks for in the repository root.
const DefaultName = ".docrot-baseline.json"

// Version is the schema version Save writes and Load accepts.
const Version = 1

// Entry is one frozen finding. Only the fingerprint takes part in matching;
// the other fields exist so a human reading the file can tell what was
// frozen and why.
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Message     string `json:"message"`
}

// File is the on-disk baseline document.
type File struct {
	Version   int       `json:"version"`
	Generated time.Time `json:"generated"`
	Findings  []Entry   `json:"findings"`
}

// Has reports whether fingerprint is frozen by this baseline. A nil *File has
// nothing frozen.
func (f *File) Has(fingerprint string) bool {
	if f == nil {
		return false
	}
	for _, e := range f.Findings {
		if e.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

// Load reads a baseline from path.
//
// A missing file is reported as an ordinary error wrapping fs.ErrNotExist, so
// callers decide whether that is fatal:
//
//	b, err := baseline.Load(path)
//	if err != nil && !errors.Is(err, fs.ErrNotExist) {
//	    return err
//	}
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("baseline %s: %w", path, err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("baseline %s: unsupported version %d (want %d)", path, f.Version, Version)
	}
	return &f, nil
}

// Save writes findings to path as a baseline: schema Version, a generation
// timestamp in UTC truncated to whole seconds, and one entry per distinct
// fingerprint sorted by fingerprint. The JSON is indented with two spaces and
// ends with a newline so the file diffs cleanly.
func Save(path string, findings []model.Finding) error {
	f := File{
		Version:   Version,
		Generated: time.Now().UTC().Truncate(time.Second),
		Findings:  Entries(findings),
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("baseline %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("baseline %s: %w", path, err)
	}
	return nil
}

// Entries converts findings into baseline entries: one per distinct
// fingerprint (the first finding wins), sorted by fingerprint. Findings
// without a fingerprint are skipped, since they could never be matched again.
func Entries(findings []model.Finding) []Entry {
	seen := make(map[string]struct{}, len(findings))
	out := make([]Entry, 0, len(findings))
	for _, f := range findings {
		if f.Fingerprint == "" {
			continue
		}
		if _, dup := seen[f.Fingerprint]; dup {
			continue
		}
		seen[f.Fingerprint] = struct{}{}
		out = append(out, Entry{
			Fingerprint: f.Fingerprint,
			Rule:        f.Rule,
			File:        f.Loc.File,
			Message:     f.Message,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out
}

// Apply marks every finding whose fingerprint appears in b as Baselined and
// clears the flag on the others, so the result does not depend on how the
// findings arrived.
//
// It returns how many findings were baselined, how many are new (that is, not
// in the baseline), and which baseline entries were not seen this run —
// "fixed" problems that can be dropped by regenerating the baseline. The
// fixed entries are sorted by fingerprint.
//
// A nil b baselines nothing: every finding counts as new.
func Apply(b *File, findings []model.Finding) (baselined, newCount int, fixed []Entry) {
	frozen := make(map[string]Entry)
	if b != nil {
		for _, e := range b.Findings {
			frozen[e.Fingerprint] = e
		}
	}
	seen := make(map[string]struct{}, len(findings))
	for i := range findings {
		fp := findings[i].Fingerprint
		_, ok := frozen[fp]
		ok = ok && fp != ""
		findings[i].Baselined = ok
		if ok {
			baselined++
			seen[fp] = struct{}{}
		} else {
			newCount++
		}
	}
	for fp, e := range frozen {
		if _, ok := seen[fp]; !ok {
			fixed = append(fixed, e)
		}
	}
	sort.Slice(fixed, func(i, j int) bool { return fixed[i].Fingerprint < fixed[j].Fingerprint })
	return baselined, newCount, fixed
}
