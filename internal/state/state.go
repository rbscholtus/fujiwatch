// Package state persists what was seen in previous runs (data/state.json)
// and the per-product price history (data/history/<product-id>.jsonl).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Offer is the last known state of one shop listing, keyed by Key(shop, url).
type Offer struct {
	Product   string    `json:"product"`
	Shop      string    `json:"shop"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	PriceVND  int64     `json:"price_vnd"`
	InStock   *bool     `json:"in_stock"`
	Condition string    `json:"condition"`
	Source    string    `json:"source"` // key of the source page it was found on
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Missed    int       `json:"missed,omitempty"` // consecutive successful runs of its source without it
}

// Source is the health of one configured source page, keyed by Key(shop, url).
type Source struct {
	LastCount int       `json:"last_count"` // offers extracted on the last successful run
	LastOK    time.Time `json:"last_ok,omitzero"`
	Failures  int       `json:"failures,omitempty"` // consecutive failed runs
	LastError string    `json:"last_error,omitempty"`
}

// State is the content of data/state.json.
type State struct {
	Offers  map[string]*Offer  `json:"offers"`
	Sources map[string]*Source `json:"sources"`
	// Flash holds the flash-sale offers already reported, keyed by
	// FlashKey, with the end of their slot; entries are dropped once the
	// slot has ended.
	Flash map[string]time.Time `json:"flash,omitempty"`
}

// FlashKey identifies a flash-sale offer in one slot: "shop|url|start".
func FlashKey(shop, url string, start time.Time) string {
	return Key(shop, url) + "|" + start.UTC().Format(time.RFC3339)
}

// Key identifies an offer or source: "shop|url".
func Key(shop, url string) string { return shop + "|" + url }

// New returns an empty state.
func New() *State {
	return &State{Offers: map[string]*Offer{}, Sources: map[string]*Source{}, Flash: map[string]time.Time{}}
}

// Load reads path; a missing file yields an empty state (first run).
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	st := New()
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("decode state %s: %w", path, err)
	}
	if st.Offers == nil {
		st.Offers = map[string]*Offer{}
	}
	if st.Sources == nil {
		st.Sources = map[string]*Source{}
	}
	if st.Flash == nil {
		st.Flash = map[string]time.Time{}
	}
	return st, nil
}

// Save writes the state atomically (temp file + rename), so a crash never
// leaves a half-written state.json. Keys are sorted, so diffs stay small.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return writeAtomic(path, append(data, '\n'))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
