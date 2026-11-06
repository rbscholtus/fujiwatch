// Package snapshot stores fetched pages under testdata/ for adapter
// development and golden tests.
package snapshot

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Meta describes a saved snapshot. It is stored next to the body as
// <name>.meta.json.
type Meta struct {
	Shop      string    `json:"shop"`
	Mode      string    `json:"mode"`
	PageURL   string    `json:"page_url"`
	FetchURL  string    `json:"fetch_url"`
	FetchedAt time.Time `json:"fetched_at"`
}

// MetaSuffix is the file suffix of snapshot metadata.
const MetaSuffix = ".meta.json"

var unsafeRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// Name derives a file name (without extension) from a page URL:
// the last path segment plus the query, e.g. "do-cu-chat" or
// "may-anh-fujifilm-page-2".
func Name(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "page"
	}
	p := strings.TrimSuffix(u.Path, "/")
	name := p[strings.LastIndex(p, "/")+1:]
	if u.RawQuery != "" {
		name += "-" + u.RawQuery
	}
	name = strings.Trim(unsafeRe.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	if name == "" {
		return "index"
	}
	return name
}

// Ext picks ".json" or ".html" for a response body.
func Ext(body []byte) string {
	t := strings.TrimSpace(string(body[:min(len(body), 64)]))
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return ".json"
	}
	return ".html"
}

// Save writes body and its metadata to dir/<shop>/<name><ext> and returns
// the body's path.
func Save(dir string, m Meta, body []byte) (string, error) {
	shopDir := filepath.Join(dir, m.Shop)
	if err := os.MkdirAll(shopDir, 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", shopDir, err)
	}
	base := filepath.Join(shopDir, Name(m.PageURL))
	bodyPath := base + Ext(body)
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		return "", fmt.Errorf("write snapshot: %w", err)
	}
	meta, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode meta: %w", err)
	}
	if err := os.WriteFile(base+MetaSuffix, append(meta, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write meta: %w", err)
	}
	return bodyPath, nil
}

// LoadMeta reads the metadata stored next to a snapshot body file.
func LoadMeta(bodyPath string) (Meta, error) {
	base := strings.TrimSuffix(bodyPath, filepath.Ext(bodyPath))
	data, err := os.ReadFile(base + MetaSuffix)
	if err != nil {
		return Meta{}, fmt.Errorf("read meta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("decode meta: %w", err)
	}
	return m, nil
}
