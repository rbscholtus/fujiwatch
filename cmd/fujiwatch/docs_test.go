package main

// Checks that the docs keep up with the code (see CLAUDE.md): the README
// tables, the shop notes, the CLI flags and relative links.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/adapters"
)

const repoRoot = "../.."

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// section returns the text from the heading line with the given title up to
// the next heading of the same or a higher level.
func section(t *testing.T, doc, title string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		level := len(l) - len(strings.TrimLeft(l, "#"))
		if level == 0 || strings.TrimSpace(l[level:]) != title {
			continue
		}
		var out []string
		for _, m := range lines[i+1:] {
			if n := len(m) - len(strings.TrimLeft(m, "#")); n > 0 && n <= level && strings.HasPrefix(m[n:], " ") {
				break
			}
			out = append(out, m)
		}
		return strings.Join(out, "\n")
	}
	t.Fatalf("no section %q", title)
	return ""
}

// tableColumn returns the first column of every row of the first table
// after the line containing marker, without backticks.
func tableColumn(t *testing.T, text, marker string) []string {
	t.Helper()
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("no %q", marker)
	}
	var col []string
	inTable := false
	for _, l := range strings.Split(text[i:], "\n")[1:] {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		cell := strings.Trim(strings.TrimSpace(strings.Split(l, "|")[1]), "`")
		if strings.Trim(cell, "-: ") == "" { // separator row
			continue
		}
		col = append(col, cell)
	}
	return col[1:] // drop the header
}

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)
	return c
}

func TestReadmeProductsTable(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(repoRoot, "config"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(cfg.Watchlist.Products))
	for _, p := range cfg.Watchlist.Products {
		ids = append(ids, p.ID)
	}
	got := tableColumn(t, readDoc(t, "README.md"), "**Watched products**")
	if !slices.Equal(sortedCopy(got), sortedCopy(ids)) {
		t.Errorf("README products table lists %v,\nconfig/watchlist.yaml has %v", sortedCopy(got), sortedCopy(ids))
	}
}

func TestReadmeShopsTable(t *testing.T) {
	got := tableColumn(t, readDoc(t, "README.md"), "**Shops**")
	if want := adapters.Shops(); !slices.Equal(sortedCopy(got), want) {
		t.Errorf("README shops table lists %v, adapters are %v", sortedCopy(got), want)
	}
}

// Every shop has a "## " section in docs/shops.md naming it.
func TestShopNotes(t *testing.T) {
	var headings []string
	for l := range strings.SplitSeq(readDoc(t, "docs/shops.md"), "\n") {
		if strings.HasPrefix(l, "## ") {
			headings = append(headings, l)
		}
	}
	for _, shop := range adapters.Shops() {
		re := regexp.MustCompile(`(^|[\s:,])` + regexp.QuoteMeta(shop) + `($|[\s,])`)
		if !slices.ContainsFunc(headings, func(h string) bool { return re.MatchString(h[3:]) }) {
			t.Errorf("docs/shops.md has no section for shop %s", shop)
		}
	}
}

// Every flag of every command is documented in docs/usage.md, CLI section.
func TestUsageDocumentsFlags(t *testing.T) {
	cli := section(t, readDoc(t, "docs/usage.md"), "CLI")
	flagRe := regexp.MustCompile(`(?m)^\s+-([a-z][a-z-]*)`)
	for _, cmd := range []string{"run", "snapshot", "extract", "match", "sitedata"} {
		var stdout, stderr bytes.Buffer
		run([]string{cmd, "-h"}, &stdout, &stderr)
		flags := flagRe.FindAllStringSubmatch(stderr.String(), -1)
		if len(flags) == 0 {
			t.Fatalf("%s -h printed no flags:\n%s", cmd, stderr.String())
		}
		for _, f := range flags {
			if !strings.Contains(cli, "--"+f[1]) {
				t.Errorf("docs/usage.md CLI section does not document %s --%s", cmd, f[1])
			}
		}
		if !strings.Contains(cli, "fujiwatch "+cmd) {
			t.Errorf("docs/usage.md CLI section does not show fujiwatch %s", cmd)
		}
	}
}

// Relative links in the docs point at files that exist, and #anchors at
// headings that exist.
func TestDocLinks(t *testing.T) {
	more, _ := filepath.Glob(filepath.Join(repoRoot, "docs", "*.md"))
	docs := make([]string, 0, 2+len(more))
	docs = append(docs, "README.md", "CLAUDE.md")
	for _, m := range more {
		docs = append(docs, filepath.Join("docs", filepath.Base(m)))
	}
	linkRe := regexp.MustCompile(`\]\(([^)\s]+)\)`)
	for _, doc := range docs {
		text := readDoc(t, doc)
		for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, anchor, _ := strings.Cut(target, "#")
			path := filepath.Join(filepath.Dir(doc), file)
			if file == "" {
				path = doc
			}
			b, err := os.ReadFile(filepath.Join(repoRoot, path))
			if err != nil {
				if _, statErr := os.Stat(filepath.Join(repoRoot, path)); statErr != nil {
					t.Errorf("%s: broken link %s", doc, target)
				}
				continue // a directory
			}
			if anchor != "" && !slices.Contains(anchors(string(b)), anchor) {
				t.Errorf("%s: link %s: no heading #%s in %s", doc, target, anchor, path)
			}
		}
	}
}

// anchors returns GitHub's heading anchors for a markdown document.
func anchors(doc string) []string {
	var out []string
	inCode := false
	for l := range strings.SplitSeq(doc, "\n") {
		if strings.HasPrefix(l, "```") {
			inCode = !inCode
		}
		if inCode || !strings.HasPrefix(l, "#") {
			continue
		}
		h := strings.ToLower(strings.TrimSpace(strings.TrimLeft(l, "#")))
		var b strings.Builder
		for _, r := range h {
			switch {
			case r == ' ':
				b.WriteRune('-')
			case r == '-' || r == '_' || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || r > 127:
				b.WriteRune(r)
			}
		}
		out = append(out, b.String())
	}
	return out
}
