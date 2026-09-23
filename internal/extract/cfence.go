package extract

import (
	"regexp"
	"strings"

	"docrot/internal/model"
)

// C and C++ code blocks: the headers they include are claims.

var cLangs = map[string]bool{"c": true, "cpp": true, "c++": true, "cxx": true, "cc": true, "h": true, "hpp": true, "objc": true, "objective-c": true, "cuda": true}

// #include "fix/fix.h"  /  #include <curl/curl.h>
var reInclude = regexp.MustCompile(`^\s*#\s*include\s*(["<])([^">]+)[">]`)

// cFenceRefs returns the include claims of one C/C++ block: a quoted
// include is a path claim at high confidence, an angle-bracket include is
// checked too but never reported when it is not part of this repository
// (the resolver treats "c:<…>" as a system header when nothing matches).
func (x *extractor) cFenceRefs(content []string, startLine int) []model.Reference {
	var out []model.Reference
	seen := map[string]bool{}
	for i, ln := range content {
		m := reInclude.FindStringSubmatch(ln)
		if m == nil || seen[m[2]] || strings.ContainsAny(m[2], " <>") {
			continue // "a libcurl header.h": prose inside the brackets
		}
		seen[m[2]] = true
		norm := "c:" + m[1] + m[2]
		conf := model.High
		if m[1] == "<" {
			conf = model.Medium
		}
		out = append(out, model.Reference{Kind: model.KindImport, Text: m[2], Norm: norm, Confidence: conf, Loc: model.Location{Line: startLine + 1 + i}})
	}
	return out
}
