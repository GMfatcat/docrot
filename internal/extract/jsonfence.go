package extract

import (
	"encoding/json"
	"io"
	"strings"

	"docrot/internal/markdown"
	"docrot/internal/model"
)

// jsonLangs are the fence languages parsed as JSON configuration examples.
var jsonLangs = map[string]bool{"json": true, "jsonc": true, "json5": true}

// jsonKey is one key path found in a JSON fence, with the content line
// (0-based, relative to the fence) it sits on.
type jsonKey struct {
	path string
	line int
}

// jsonFenceRefs turns a ```json block into config-key references when the
// block looks like a configuration example: at least one of its top-level
// keys is a key of a config struct or sample. Keys whose parent is unknown
// are not emitted (the parent is reported once, instead of every child).
// The confidence is high when at least half the top-level keys are known,
// medium otherwise; the resolver maps that to warning/info.
func (x *extractor) jsonFenceRefs(f markdown.Fence) []model.Reference {
	keys := walkJSONKeys(lenientJSON(f.Content))
	if len(keys) == 0 {
		return nil
	}
	known := func(p string) bool { return x.hints.HasJSONKey(p) || x.hints.HasConfigKey(p) }
	top, knownTop := 0, 0
	for _, k := range keys {
		if !strings.Contains(k.path, ".") {
			top++
			if known(k.path) {
				knownTop++
			}
		}
	}
	if knownTop == 0 {
		return nil // a response body, a package.json, an unrelated example
	}
	conf := model.Medium
	if knownTop*2 >= top {
		conf = model.High
	}
	var out []model.Reference
	for _, k := range keys {
		if i := strings.LastIndex(k.path, "."); i >= 0 && !known(k.path[:i]) {
			continue
		}
		r := model.Reference{Kind: model.KindConfigKey, Text: k.path, Norm: k.path, Confidence: conf}
		r.Loc.Line = f.StartLine + 1 + k.line // Loc is completed by emit; Line carries the offset
		out = append(out, r)
	}
	return out
}

// walkJSONKeys streams the tokens of a JSON document and returns every
// object key as a dotted path, arrays contributing no segment
// ("servers.addr" for {"servers": [{"addr": ...}]}). A document that does
// not parse yields nothing.
func walkJSONKeys(text string) []jsonKey {
	type frame struct {
		object    bool
		expectKey bool
		path      string // path of this container
		key       string // last key read (objects)
	}
	dec := json.NewDecoder(strings.NewReader(text))
	var stack []frame
	var out []jsonKey
	lines, lastOff := 0, int64(0)
	lineAt := func() int {
		off := dec.InputOffset()
		if off > lastOff {
			lines += strings.Count(text[lastOff:off], "\n")
			lastOff = off
		}
		return lines
	}
	cur := func() string {
		if len(stack) == 0 {
			return ""
		}
		top := stack[len(stack)-1]
		if !top.object {
			return top.path
		}
		if top.path == "" {
			return top.key
		}
		return top.path + "." + top.key
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, frame{object: true, expectKey: true, path: cur()})
			case '[':
				stack = append(stack, frame{path: cur()})
			case '}', ']':
				if len(stack) == 0 {
					return nil
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].expectKey = true
				}
			}
		default:
			if len(stack) == 0 {
				continue // a bare scalar document
			}
			top := &stack[len(stack)-1]
			if top.object && top.expectKey {
				key, ok := t.(string)
				if !ok {
					return nil
				}
				top.key = key
				top.expectKey = false
				out = append(out, jsonKey{path: cur(), line: lineAt()})
				continue
			}
			if top.object {
				top.expectKey = true
			}
		}
	}
	return out
}

// lenientJSON makes documentation JSON parseable: // and /* */ comments
// become spaces (newlines are kept so line numbers survive), trailing
// commas are dropped, "..." and "…" placeholders become null, and a
// fragment that starts with a key ("addr": ":8080",) is wrapped in braces.
func lenientJSON(lines []string) string {
	src := strings.Join(lines, "\n")
	var b strings.Builder
	b.Grow(len(src) + 2)
	inStr := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inStr:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			i++ // the '/'
		case c == '.' && strings.HasPrefix(src[i:], "..."):
			b.WriteString("null")
			i += 2
		case strings.HasPrefix(src[i:], "…"):
			b.WriteString("null")
			i += len("…") - 1
		default:
			b.WriteByte(c)
		}
	}
	s := b.String()
	if t := strings.TrimSpace(s); strings.HasPrefix(t, "\"") {
		s = "{" + s + "}"
	}
	return dropTrailingCommas(s)
}

// dropTrailingCommas removes a comma that is followed only by whitespace
// and a closing bracket, outside string literals.
func dropTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == ',':
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				b.WriteByte(' ')
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
