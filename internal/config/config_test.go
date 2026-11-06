package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRepoConfig(t *testing.T) {
	cfg, err := Load("../../config/watchlist.yaml", "../../config/sources.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Watchlist.Products) == 0 {
		t.Fatal("no products loaded")
	}
	if len(cfg.Sources.Sources) == 0 {
		t.Fatal("no sources loaded")
	}
	for _, s := range cfg.Sources.Sources {
		if s.Condition == "" {
			t.Errorf("source %s %s: condition default not applied", s.Shop, s.URL)
		}
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const validWatchlist = `
products:
  - id: x-t5
    name: "Fujifilm X-T5"
    kind: body
    patterns: ['\bx-t5(\D|$)']
`

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	wl := writeFile(t, dir, "watchlist.yaml", validWatchlist)
	src := writeFile(t, dir, "sources.yaml", `
sources:
  - shop: a
    mode: product
    product: x-t5
    url: https://a.example/p
  - shop: b
    mode: listing
    url: https://b.example/used
`)

	cfg, err := Load(wl, src)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Sources.Sources[0].Condition; got != ConditionNew {
		t.Errorf("product source condition = %q, want %q", got, ConditionNew)
	}
	if got := cfg.Sources.Sources[1].Condition; got != ConditionAuto {
		t.Errorf("listing source condition = %q, want %q", got, ConditionAuto)
	}
	if got := cfg.Sources.Defaults.Delay(); got != DefaultDelay {
		t.Errorf("Delay() = %v, want %v", got, DefaultDelay)
	}
	if got := cfg.Sources.Defaults.Timeout(); got != DefaultTimeout {
		t.Errorf("Timeout() = %v, want %v", got, DefaultTimeout)
	}
}

func TestDefaultsDurations(t *testing.T) {
	d := Defaults{DelayMS: 2500, TimeoutS: 7}
	if got := d.Delay(); got != 2500*time.Millisecond {
		t.Errorf("Delay() = %v", got)
	}
	if got := d.Timeout(); got != 7*time.Second {
		t.Errorf("Timeout() = %v", got)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name      string
		watchlist string
		sources   string
		wantErr   []string
	}{
		{
			name:      "unknown yaml key",
			watchlist: validWatchlist + "    require-all: ['wr']\n",
			sources:   "sources: []",
			wantErr:   []string{"field require-all not found"},
		},
		{
			name: "bad regex, duplicate id, no patterns",
			watchlist: `
products:
  - id: a
    patterns: ['(']
  - id: a
`,
			sources: "sources: []",
			wantErr: []string{"missing closing )", "duplicate id", "at least one pattern"},
		},
		{
			name:      "bad sources",
			watchlist: validWatchlist,
			sources: `
sources:
  - shop: a
    mode: product
    product: nope
    url: https://a.example/p
  - shop: b
    mode: catalog
    url: ftp://b.example/
  - mode: listing
    product: x-t5
    condition: mint
    url: https://c.example/
`,
			wantErr: []string{
				`unknown product "nope"`,
				`mode must be`,
				`invalid url "ftp://b.example/"`,
				"shop is required",
				"product must be empty in listing mode",
				`invalid condition "mint"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := Load(
				writeFile(t, dir, "watchlist.yaml", tt.watchlist),
				writeFile(t, dir, "sources.yaml", tt.sources),
			)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
