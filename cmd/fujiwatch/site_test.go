package main

// Spec tests for README §11 (history site) and the site job of README §10.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// End to end: run (from snapshots) → sitedata → JSON for every product.
func TestSiteDataCommand(t *testing.T) {
	useSnapshotFetcher(t)
	data, out := t.TempDir(), t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "--no-notify", "--config", "../../config", "--data", data}, &stdout, &stderr); code != exitOK {
		t.Fatalf("run: exit %d: %s", code, stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"sitedata", "--config", "../../config", "--data", data, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("sitedata: exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote 6 products") {
		t.Errorf("stdout = %q", stdout.String())
	}

	var idx struct {
		Products []struct {
			ID     string            `json:"id"`
			Points int               `json:"points"`
			Offers []json.RawMessage `json:"offers"`
		} `json:"products"`
	}
	b, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		t.Fatal(err)
	}
	for _, p := range idx.Products {
		if _, err := os.Stat(filepath.Join(out, p.ID+".json")); err != nil {
			t.Errorf("missing %s.json", p.ID)
		}
		if p.Points == 0 || len(p.Offers) == 0 {
			t.Errorf("%s: %d points, %d offers; every watched product is in the snapshots", p.ID, p.Points, len(p.Offers))
		}
	}

	// Empty data dir (first deploy before any run) still produces a site.
	stdout.Reset()
	if code := run([]string{"sitedata", "--config", "../../config", "--data", t.TempDir(), "--out", t.TempDir()}, &stdout, &stderr); code != exitOK {
		t.Errorf("sitedata on empty data: exit %d: %s", code, stderr.String())
	}
}

// README §11: Svelte + Vite, static build, small and without a chart library.
func TestSiteProject(t *testing.T) {
	var pkg struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	b, err := os.ReadFile("../../site/package.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Scripts["build"] != "vite build" {
		t.Errorf("build script = %q", pkg.Scripts["build"])
	}
	for _, dep := range []string{"svelte", "vite", "@sveltejs/vite-plugin-svelte"} {
		v := pkg.DevDependencies[dep]
		if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
			t.Errorf("%s version %q should be pinned exactly", dep, v)
		}
	}
	if len(pkg.Dependencies) != 0 {
		t.Errorf("runtime dependencies %v: the site should only need build tools", pkg.Dependencies)
	}
	if _, err := os.Stat("../../site/package-lock.json"); err != nil {
		t.Error("site/package-lock.json is required for npm ci")
	}

	cfg, err := os.ReadFile("../../site/vite.config.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `base: "./"`) {
		t.Error(`vite base must be "./" so the build works under /<repo>/ on GitHub Pages`)
	}

	gi, err := os.ReadFile("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"site/node_modules/", "site/dist/", "site/public/data/"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf(".gitignore should ignore %s", want)
		}
	}
}

func TestWatchWorkflowSiteJob(t *testing.T) {
	wf := readWorkflow(t, "watch.yml")
	_, site, ok := strings.Cut(wf, "\n  site:")
	if !ok {
		t.Fatal("watch.yml has no site job")
	}
	for _, want := range []string{
		"needs: scrape",
		"ref: main",
		"go run ./cmd/fujiwatch sitedata --out site/public/data",
		"npm ci && npm run build",
		"actions/upload-pages-artifact@",
		"path: site/dist",
		"actions/deploy-pages@",
		"name: github-pages",
		"if: ${{ !inputs.dry_run }}",
	} {
		if !strings.Contains(site, want) {
			t.Errorf("site job missing %q", want)
		}
	}
	// Site data must be generated before the Vite build reads it.
	if strings.Index(site, "sitedata") > strings.Index(site, "npm run build") {
		t.Error("sitedata must run before the site build")
	}
	for _, perm := range []string{"pages: write", "id-token: write"} {
		if !strings.Contains(wf, perm) {
			t.Errorf("watch.yml lacks permission %q for Pages deployment", perm)
		}
	}
	if !strings.Contains(readWorkflow(t, "ci.yml"), "npm ci && npm run build") {
		t.Error("ci.yml should build the site on pull requests")
	}
}
