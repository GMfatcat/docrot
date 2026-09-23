package markdown

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	customIDRe   = regexp.MustCompile(`\s*\{\s*#[^}]*\}\s*$`)
	inlineLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	refLinkRe    = regexp.MustCompile(`!?\[([^\]]*)\]\[[^\]]*\]`)
	htmlTagRe    = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	customIDCap  = regexp.MustCompile(`\{\s*#([^}\s]+)\s*\}`)
)

// CustomID returns the explicit anchor id of a heading written in the
// Markdown-extensions style "## Title { #my-id }" (MkDocs, Python-Markdown),
// or "" when there is none.
func CustomID(heading string) string {
	if m := customIDCap.FindStringSubmatch(heading); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// Slug converts heading text into a GitHub anchor slug.
//
// The rules are GitHub's: lower-case everything, drop every character that
// is not a Unicode letter, digit, underscore, space or hyphen (so CJK
// survives and emoji do not), and turn spaces into hyphens. Runs of hyphens are *not*
// collapsed and leading/trailing hyphens are *not* trimmed, which is why
// "🐱 meowbase" becomes "-meowbase".
//
// Before those rules are applied Slug also removes a trailing "{#custom-id}"
// attribute, unwraps inline and reference links to their text, and drops raw
// HTML tags, so that "## [Setup](#setup)" and `## <a name="x"></a> Setup`
// both slug to "setup". Inline code backticks disappear with the rest of the
// punctuation, keeping the text inside them.
//
// Duplicate suffixes ("-1", "-2") are added by Parse, not here.
func Slug(heading string) string {
	s := customIDRe.ReplaceAllString(heading, "")
	s = inlineLinkRe.ReplaceAllString(s, "$1")
	s = refLinkRe.ReplaceAllString(s, "$1")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "`", "")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	return b.String()
}
