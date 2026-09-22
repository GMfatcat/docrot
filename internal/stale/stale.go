// Package stale scores document sections against git history (design §11).
//
// A section is suspicious when the code it talks about kept moving after the
// section itself was last touched: docrot blames the document to learn when
// each section was edited, then counts the commits that landed on the
// referenced files since then. Churn (many commits) or age (a change long
// after the section was written) makes the section stale.
//
// Documents git does not know about are skipped silently, because staleness
// is an optional, git-backed accelerator and never a hard requirement.
package stale

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"docrot/internal/gitx"
	"docrot/internal/markdown"
	"docrot/internal/model"
)

// Defaults used when the matching field of [Options] is zero.
const (
	DefaultMinChurn = 3
	DefaultMinDays  = 90
)

// concurrency bounds the goroutines asking git about churn. gitx limits the
// number of git processes by itself; this only keeps the goroutine count in
// check on documents with hundreds of references.
const concurrency = 4

// maxListedFiles is how many files the message enumerates.
const maxListedFiles = 5

// dateLayout is the ISO date used in messages; times are rendered in UTC so
// that a report does not depend on the machine's time zone.
const dateLayout = "2006-01-02"

// Options tunes the staleness thresholds.
type Options struct {
	// MinChurn is the number of commits since the section was edited that
	// makes it stale on its own. Zero means DefaultMinChurn.
	MinChurn int
	// MinDays is the age, in days, of the newest change that makes a single
	// commit enough to call the section stale. Zero means DefaultMinDays.
	MinDays int
	// Severity of the findings. Empty means model.SevWarning.
	Severity model.Severity
	// Now is the reference clock. Zero means time.Now(). It is not part of
	// the staleness decision, which compares commit times with the section's
	// own edit time only.
	Now time.Time
}

// normalized fills in the defaults.
func (o Options) normalized() Options {
	if o.MinChurn <= 0 {
		o.MinChurn = DefaultMinChurn
	}
	if o.MinDays <= 0 {
		o.MinDays = DefaultMinDays
	}
	if o.Severity == "" {
		o.Severity = model.SevWarning
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	return o
}

// ResolvedRef is a reference that resolved to a file or directory in the
// repo. File is relative, uses forward slashes and may name a directory; a
// symbol reference carries the file that defines it.
type ResolvedRef struct {
	Ref  model.Reference
	File string
}

// section is one slice of the document: the lines of a heading and
// everything below it up to the next heading.
type section struct {
	name  string // heading text; "" for the part before the first heading
	line  int    // line reported in the finding
	start int
	end   int
}

// fileStat is the churn of one referenced file since a section's edit time.
type fileStat struct {
	file    string
	commits int
	latest  time.Time
}

// Analyze returns one stale-section finding per section of docRel whose
// referenced code churned after the section was last edited. repo must be
// non-nil.
//
// The document is blamed once; a section's edit time is the newest commit
// time among its lines. A section holding an uncommitted line is skipped
// entirely: it is being edited right now, so any verdict would be wrong.
// When git does not know docRel at all the document is skipped and no error
// is reported.
//
// The message lists at most five files, the most churned first; Data carries
// every changed file.
func Analyze(repo *gitx.Repo, doc *markdown.Doc, docRel string, refs []ResolvedRef, opts Options) ([]model.Finding, error) {
	if repo == nil {
		return nil, errors.New("stale: nil repo")
	}
	if doc == nil {
		return nil, nil
	}
	opts = opts.normalized()

	times, err := repo.BlameLineTimes(docRel)
	if err != nil {
		if errors.Is(err, gitx.ErrUnavailable) {
			return nil, nil
		}
		return nil, err
	}

	type work struct {
		sec    section
		edited time.Time
		stats  []fileStat
	}
	var todo []work
	for _, sec := range sections(doc) {
		edited, ok := sectionTime(times, sec.start, sec.end)
		if !ok {
			continue
		}
		files := sectionFiles(refs, sec)
		if len(files) == 0 {
			continue
		}
		stats := make([]fileStat, len(files))
		for i, f := range files {
			stats[i] = fileStat{file: f}
		}
		todo = append(todo, work{sec: sec, edited: edited, stats: stats})
	}
	if len(todo) == 0 {
		return nil, nil
	}

	var (
		wg   sync.WaitGroup
		sem  = make(chan struct{}, concurrency)
		mu   sync.Mutex
		errs []error
	)
	for wi := range todo {
		for si := range todo[wi].stats {
			wg.Add(1)
			go func(w *work, s *fileStat) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				commits, err := repo.CommitsSince(s.file, w.edited)
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					return
				}
				s.commits = len(commits)
				if len(commits) > 0 {
					s.latest = commits[0].Time
				}
			}(&todo[wi], &todo[wi].stats[si])
		}
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}

	var out []model.Finding
	for _, w := range todo {
		changed := changedFiles(w.stats)
		if !isStale(changed, w.edited, opts) {
			continue
		}
		out = append(out, finding(docRel, w.sec, w.edited, changed, opts.Severity))
	}
	return out, nil
}

// sections splits the document at its headings. The lines before the first
// heading form a leading section with an empty name, reported at line 1.
func sections(doc *markdown.Doc) []section {
	n := len(doc.Lines)
	if n == 0 {
		return nil
	}
	hs := doc.Headings
	if len(hs) == 0 {
		return []section{{line: 1, start: 1, end: n}}
	}
	var out []section
	if hs[0].Line > 1 {
		out = append(out, section{line: 1, start: 1, end: hs[0].Line - 1})
	}
	for i, h := range hs {
		end := n
		if i+1 < len(hs) {
			end = hs[i+1].Line - 1
		}
		out = append(out, section{name: h.Text, line: h.Line, start: h.Line, end: end})
	}
	return out
}

// sectionTime returns the newest blame time among the lines of the section.
// ok is false when the section is empty, when a line is not committed yet or
// when the blame output is shorter than the document, all of which mean the
// section is being edited.
func sectionTime(times []time.Time, start, end int) (time.Time, bool) {
	var newest time.Time
	for l := start; l <= end; l++ {
		if l < 1 || l >= len(times) || times[l].IsZero() {
			return time.Time{}, false
		}
		if times[l].After(newest) {
			newest = times[l]
		}
	}
	return newest, !newest.IsZero()
}

// sectionFiles returns the distinct files referenced from the section, in
// first-appearance order.
func sectionFiles(refs []ResolvedRef, sec section) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if r.File == "" || r.Ref.Loc.Line < sec.start || r.Ref.Loc.Line > sec.end {
			continue
		}
		if seen[r.File] {
			continue
		}
		seen[r.File] = true
		out = append(out, r.File)
	}
	return out
}

// changedFiles keeps the files with at least one commit, most churned first,
// ties broken by name.
func changedFiles(stats []fileStat) []fileStat {
	out := make([]fileStat, 0, len(stats))
	for _, s := range stats {
		if s.commits > 0 {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].commits != out[j].commits {
			return out[i].commits > out[j].commits
		}
		return out[i].file < out[j].file
	})
	return out
}

// isStale applies the two thresholds: enough commits, or a change that
// landed long enough after the section was written.
func isStale(changed []fileStat, edited time.Time, opts Options) bool {
	age := time.Duration(opts.MinDays) * 24 * time.Hour
	for _, s := range changed {
		if s.commits >= opts.MinChurn {
			return true
		}
		if s.commits >= 1 && s.latest.Sub(edited) >= age {
			return true
		}
	}
	return false
}

// finding builds the stale-section finding for one section.
func finding(docRel string, sec section, edited time.Time, changed []fileStat, sev model.Severity) model.Finding {
	files := make([]map[string]any, 0, len(changed))
	for _, s := range changed {
		files = append(files, map[string]any{
			"file":    s.file,
			"commits": s.commits,
			"latest":  s.latest.UTC().Format(time.RFC3339),
		})
	}
	return model.Finding{
		Rule:        model.RuleStaleSection,
		Severity:    sev,
		Message:     message(sec.name, edited, changed),
		Loc:         model.Location{File: docRel, Line: sec.line},
		Fingerprint: model.Fingerprint(model.RuleStaleSection, docRel, markdown.Slug(sec.name)),
		Data: map[string]any{
			"section": sec.name,
			"edited":  edited.UTC().Format(time.RFC3339),
			"files":   files,
		},
	}
}

// message renders the human-readable summary of a stale section.
func message(name string, edited time.Time, changed []fileStat) string {
	var b strings.Builder
	fmt.Fprintf(&b, "section %q last edited %s; since then ", name, edited.UTC().Format(dateLayout))
	for i, s := range changed {
		if i >= maxListedFiles {
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s changed %d× (latest %s)", s.file, s.commits, s.latest.UTC().Format(dateLayout))
	}
	return b.String()
}
