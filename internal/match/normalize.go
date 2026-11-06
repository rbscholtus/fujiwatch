// Package match normalises product titles and matches them against the
// watchlist.
package match

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// "f/2.8", "F 2.8" → "f2.8"
	apertureRe = regexp.MustCompile(`\bf\s*/\s*(\d)`)
	// "16 - 50", "16–50" (after dash folding) → "16-50"
	rangeRe = regexp.MustCompile(`(\d)\s*-\s*(\d)`)
	// Fujifilm body names: "xt5", "x t5", "x-t5", "X-H2S", "x pro3" → "x-t5",
	// "x-h2s", "x-pro3". Only body lines are listed, so lens prefixes like
	// "xf10-24" and "xc15-45" are left alone.
	bodyRe  = regexp.MustCompile(`\bx[\s-]?(t|s|h|e|m|a|pro)\s?(\d)`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// stripMarks removes combining diacritics after NFD decomposition.
var stripMarks = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// dashFolder maps the dash and space variants shops use to plain ASCII.
var dashFolder = strings.NewReplacer(
	"‐", "-", // hyphen
	"‑", "-", // non-breaking hyphen
	"‒", "-", // figure dash
	"–", "-", // en dash
	"—", "-", // em dash
	"−", "-", // minus sign
	" ", " ", // no-break space
)

// Normalize turns a shop title (or URL) into the canonical form patterns are
// written against: lowercase ASCII without Vietnamese diacritics, unified
// dashes and spacing, and canonical Fujifilm body names.
//
//	"Ống kính Fujinon XF10–24mm F/4 R OIS WR" → "ong kinh fujinon xf10-24mm f4 r ois wr"
//	"Máy ảnh Fujifilm XT5 (Đen)"              → "may anh fujifilm x-t5 (den)"
func Normalize(s string) string {
	s = strings.ToLower(s)
	// NFD does not decompose đ, so map it explicitly.
	s = strings.ReplaceAll(s, "đ", "d")
	if out, _, err := transform.String(stripMarks, s); err == nil {
		s = out
	}
	s = dashFolder.Replace(s)
	s = apertureRe.ReplaceAllString(s, "f$1")
	s = rangeRe.ReplaceAllString(s, "$1-$2")
	s = bodyRe.ReplaceAllString(s, "x-$1$2")
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
