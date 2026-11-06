// Package config loads and validates config/watchlist.yaml and
// config/sources.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Watch modes for a source.
const (
	ModeProduct = "product"
	ModeListing = "listing"
)

// Conditions for a source.
const (
	ConditionNew  = "new"
	ConditionUsed = "used"
	ConditionAuto = "auto"
)

// Default fetch settings, used when sources.yaml leaves them out.
const (
	DefaultDelay   = 3 * time.Second
	DefaultTimeout = 20 * time.Second
)

// Product is one watched item from watchlist.yaml. All pattern fields are
// RE2 regular expressions matched against the normalised title.
type Product struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Kind string `yaml:"kind"` // body | lens

	Patterns   []string `yaml:"patterns"`    // all must match
	RequireAll []string `yaml:"require_all"` // all must match
	RequireAny []string `yaml:"require_any"` // at least one must match, if set
	Exclude    []string `yaml:"exclude"`     // none may match

	// Optional alert thresholds: a below_target event fires when an offer's
	// price drops to or below the target (once per crossing).
	TargetVND     int64 `yaml:"target_vnd"`      // new offers
	TargetUsedVND int64 `yaml:"target_used_vnd"` // used offers
}

// Target returns the target price for a condition (0 = none).
func (p Product) Target(condition string) int64 {
	if condition == ConditionUsed {
		return p.TargetUsedVND
	}
	return p.TargetVND
}

// Watchlist is the parsed watchlist.yaml.
type Watchlist struct {
	Products []Product `yaml:"products"`
}

// Defaults holds per-run fetch settings from sources.yaml.
type Defaults struct {
	DelayMS  int `yaml:"delay_ms"`
	TimeoutS int `yaml:"timeout_s"`

	// A price drop is alerted if it is at least MinDropPct percent OR at
	// least MinDropVND. Both 0 (default): every drop is alerted.
	MinDropPct float64 `yaml:"min_drop_pct"`
	MinDropVND int64   `yaml:"min_drop_vnd"`
}

// Delay returns the delay between requests to the same host.
func (d Defaults) Delay() time.Duration {
	if d.DelayMS <= 0 {
		return DefaultDelay
	}
	return time.Duration(d.DelayMS) * time.Millisecond
}

// Timeout returns the per-request timeout.
func (d Defaults) Timeout() time.Duration {
	if d.TimeoutS <= 0 {
		return DefaultTimeout
	}
	return time.Duration(d.TimeoutS) * time.Second
}

// Source is one page to watch.
type Source struct {
	Shop      string `yaml:"shop"`
	Mode      string `yaml:"mode"`      // product | listing
	Product   string `yaml:"product"`   // product mode only: watchlist ID
	Condition string `yaml:"condition"` // new | used | auto (default: new for product, auto for listing)
	URL       string `yaml:"url"`
}

// Email policies.
const (
	EmailAlways  = "always"
	EmailChanges = "changes"
)

// Notify configures notifications. Credentials come from the environment.
type Notify struct {
	// Email is "always" (default: a digest every run) or "changes" (only
	// when there are events, plus a heartbeat on the Heartbeat weekday).
	Email     string `yaml:"email"`
	Heartbeat string `yaml:"heartbeat"` // e.g. "sunday" (Vietnam time)
	// Weekly adds a "this week vs last week" summary to the email on this
	// weekday (Vietnam time); the email is always sent that day.
	Weekly string `yaml:"weekly"`
}

// Sources is the parsed sources.yaml.
type Sources struct {
	Defaults Defaults `yaml:"defaults"`
	Notify   Notify   `yaml:"notify"`
	Sources  []Source `yaml:"sources"`
}

// Config is the full configuration.
type Config struct {
	Watchlist Watchlist
	Sources   Sources
}

// Load reads and validates both config files.
func Load(watchlistPath, sourcesPath string) (*Config, error) {
	var cfg Config
	if err := decodeFile(watchlistPath, &cfg.Watchlist); err != nil {
		return nil, err
	}
	if err := decodeFile(sourcesPath, &cfg.Sources); err != nil {
		return nil, err
	}
	cfg.Sources.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ProductByID returns the product with the given ID.
func (c *Config) ProductByID(id string) (Product, bool) {
	i := slices.IndexFunc(c.Watchlist.Products, func(p Product) bool { return p.ID == id })
	if i < 0 {
		return Product{}, false
	}
	return c.Watchlist.Products[i], true
}

// Validate checks the configuration and reports all problems at once.
func (c *Config) Validate() error {
	var errs []error
	errs = append(errs, c.Watchlist.validate()...)
	if n := c.Sources.Notify; n.Email != "" && n.Email != EmailAlways && n.Email != EmailChanges {
		errs = append(errs, fmt.Errorf("notify: email must be %q or %q, got %q", EmailAlways, EmailChanges, n.Email))
	}
	if h := c.Sources.Notify.Heartbeat; h != "" && !isWeekday(h) {
		errs = append(errs, fmt.Errorf("notify: heartbeat must be a weekday name, got %q", h))
	}
	if w := c.Sources.Notify.Weekly; w != "" && !isWeekday(w) {
		errs = append(errs, fmt.Errorf("notify: weekly must be a weekday name, got %q", w))
	}
	if d := c.Sources.Defaults; d.MinDropPct < 0 || d.MinDropPct >= 100 || d.MinDropVND < 0 {
		errs = append(errs, fmt.Errorf("defaults: min_drop_pct must be in [0, 100) and min_drop_vnd ≥ 0"))
	}

	for i, s := range c.Sources.Sources {
		where := fmt.Sprintf("sources[%d] (%s)", i, s.Shop)
		if s.Shop == "" {
			errs = append(errs, fmt.Errorf("%s: shop is required", where))
		}
		if u, err := url.Parse(s.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s: invalid url %q", where, s.URL))
		}
		switch s.Mode {
		case ModeProduct:
			if _, ok := c.ProductByID(s.Product); !ok {
				errs = append(errs, fmt.Errorf("%s: unknown product %q", where, s.Product))
			}
		case ModeListing:
			if s.Product != "" {
				errs = append(errs, fmt.Errorf("%s: product must be empty in listing mode", where))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: mode must be %q or %q, got %q", where, ModeProduct, ModeListing, s.Mode))
		}
		if !slices.Contains([]string{ConditionNew, ConditionUsed, ConditionAuto}, s.Condition) {
			errs = append(errs, fmt.Errorf("%s: invalid condition %q", where, s.Condition))
		}
	}

	return errors.Join(errs...)
}

func (w Watchlist) validate() []error {
	var errs []error
	seen := map[string]bool{}
	for i, p := range w.Products {
		where := fmt.Sprintf("products[%d] (%s)", i, p.ID)
		switch {
		case p.ID == "":
			errs = append(errs, fmt.Errorf("%s: id is required", where))
		case seen[p.ID]:
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		}
		seen[p.ID] = true
		if p.TargetVND < 0 || p.TargetUsedVND < 0 {
			errs = append(errs, fmt.Errorf("%s: target prices must be ≥ 0", where))
		}
		if len(p.Patterns) == 0 {
			errs = append(errs, fmt.Errorf("%s: at least one pattern is required", where))
		}
		for _, group := range [][]string{p.Patterns, p.RequireAll, p.RequireAny, p.Exclude} {
			for _, expr := range group {
				if _, err := regexp.Compile(expr); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", where, err))
				}
			}
		}
	}
	return errs
}

func (s *Sources) applyDefaults() {
	for i := range s.Sources {
		src := &s.Sources[i]
		if src.Condition != "" {
			continue
		}
		if src.Mode == ModeProduct {
			src.Condition = ConditionNew
		} else {
			src.Condition = ConditionAuto
		}
	}
}

// decodeFile decodes YAML strictly: unknown keys are an error, so typos such
// as "require-all" don't silently disable a rule.
func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func isWeekday(s string) bool {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), s) {
			return true
		}
	}
	return false
}
