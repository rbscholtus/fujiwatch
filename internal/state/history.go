package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Record is one line of data/history/<product-id>.jsonl: one offer seen in
// one run.
type Record struct {
	TS        time.Time `json:"ts"`
	Shop      string    `json:"shop"`
	URL       string    `json:"url"`
	PriceVND  int64     `json:"price_vnd"`
	InStock   *bool     `json:"in_stock"`
	Condition string    `json:"condition"`
}

var productIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// AppendHistory appends records to dir/<product-id>.jsonl, grouped by product.
func AppendHistory(dir string, byProduct map[string][]Record) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	ids := make([]string, 0, len(byProduct))
	for id := range byProduct {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		if !productIDRe.MatchString(id) {
			return fmt.Errorf("invalid product id %q for a history file name", id)
		}
		if err := appendLines(filepath.Join(dir, id+".jsonl"), byProduct[id]); err != nil {
			return err
		}
	}
	return nil
}

func appendLines(path string, recs []Record) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			_ = f.Close()
			return fmt.Errorf("encode history: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// ReadHistory reads all records of one product; a missing file yields none.
func ReadHistory(dir, productID string) ([]Record, error) {
	f, err := os.Open(filepath.Join(dir, productID+".jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open history: %w", err)
	}
	defer func() { _ = f.Close() }()

	var recs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s.jsonl line %d: %w", productID, line, err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return recs, nil
}
