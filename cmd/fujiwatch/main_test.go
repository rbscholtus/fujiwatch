package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"match", "--config", "../../config", "Fujifilm XT3 body cũ 98%"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{
		"normalised: fujifilm x-t3 body cu 98%",
		"result:     matched x-t3",
		"condition:  used",
		"✓  x-t3",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != exitUsage {
		t.Errorf("no args: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"bogus"}, &out, &errOut); code != exitUsage {
		t.Errorf("unknown command: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"match"}, &out, &errOut); code != exitUsage {
		t.Errorf("match without title: exit %d, want %d", code, exitUsage)
	}
}

// docs/usage.md, Adding a shop, step 3: extract runs one adapter on a saved file.
func TestExtractCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"extract", "--config", "../../config", "vjshop", "../../testdata/vjshop/may-anh-fujifilm.html"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{
		"x-t5", "36990000", "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang", "(unmatched)", "36 offers",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestExtractCommandNeedsURL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(file, []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"extract", "--config", "../../config", "kyma", file}, &out, &errOut); code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "no --url given") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

// docs/usage.md, Adding a shop, step 2, against a local server instead of a real shop.
func TestSnapshotCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/x-t5.js" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"title":"Fujifilm X-T5","price":3699000000,"available":true}`))
	}))
	defer srv.Close()

	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"snapshot", "--config", "../../config", "--out", out,
		"--shop", "xspace", "--mode", "product", srv.URL + "/products/x-t5",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	saved := filepath.Join(out, "xspace", "x-t5.json")
	if !strings.Contains(stdout.String(), saved) {
		t.Errorf("stdout = %q, want it to mention %s", stdout.String(), saved)
	}

	// The saved snapshot round-trips through extract.
	stdout.Reset()
	if code := run([]string{"extract", "--config", "../../config", "xspace", saved}, &stdout, &stderr); code != exitOK {
		t.Fatalf("extract exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "x-t5") || !strings.Contains(stdout.String(), "36990000") {
		t.Errorf("extract output:\n%s", stdout.String())
	}
}

func TestResolveSource(t *testing.T) {
	cfg, err := loadConfig("../../config")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ url, shop, mode string }{
		{"https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii", "xspace", "product"},
		{"https://www.vjshop.vn/may-anh-fujifilm", "vjshop", "listing"},
		{"https://kyma.vn/ong-kinh-fujifilm", "kyma", "listing"},
	}
	for _, tt := range tests {
		shop, mode, err := resolveSource(cfg, tt.url)
		if err != nil || shop != tt.shop || mode != tt.mode {
			t.Errorf("resolveSource(%q) = %q %q %v, want %q %q", tt.url, shop, mode, err, tt.shop, tt.mode)
		}
	}
	if _, _, err := resolveSource(cfg, "not a url"); err == nil {
		t.Error("expected error for invalid url")
	}
}
