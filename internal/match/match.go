package match

import (
	"fmt"
	"regexp"

	"github.com/rbscholtus/fujiwatch/internal/config"
)

// Status is the outcome of matching one title.
type Status int

// Match outcomes.
const (
	Unmatched Status = iota // no product matched
	Matched                 // exactly one product matched
	Ambiguous               // more than one product matched
)

func (s Status) String() string {
	switch s {
	case Unmatched:
		return "unmatched"
	case Matched:
		return "matched"
	case Ambiguous:
		return "ambiguous"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// Result is the outcome of matching one title against the whole watchlist.
type Result struct {
	Normalized string
	Status     Status
	ProductID  string   // set only when Status == Matched
	Candidates []string // every product that matched (len > 1 if Ambiguous)
	// NearMiss lists products whose patterns all match but whose
	// require_all / require_any rules do not: the title names the product
	// without confirming the mount or version. Titles rejected by an
	// exclude (kits, other mounts) are deliberate rejections, not near
	// misses. Only set when Status == Unmatched.
	NearMiss []string
}

// Trace explains why a title did or did not match one product.
type Trace struct {
	ProductID string
	Matched   bool
	Reason    string // first failing rule, or "all rules pass"
}

type rule struct {
	id          string
	patterns    []*regexp.Regexp
	requireAll  []*regexp.Regexp
	requireAny  []*regexp.Regexp
	exclude     []*regexp.Regexp
	otherMounts []*regexp.Regexp
}

// Matcher matches titles against compiled watchlist rules.
type Matcher struct {
	rules []rule
}

// New compiles the watchlist into a Matcher.
func New(products []config.Product) (*Matcher, error) {
	m := &Matcher{rules: make([]rule, 0, len(products))}
	for _, p := range products {
		r := rule{id: p.ID}
		var err error
		if r.patterns, err = compileAll(p.ID, p.Patterns); err != nil {
			return nil, err
		}
		if r.requireAll, err = compileAll(p.ID, p.RequireAll); err != nil {
			return nil, err
		}
		if r.requireAny, err = compileAll(p.ID, p.RequireAny); err != nil {
			return nil, err
		}
		if r.exclude, err = compileAll(p.ID, p.Exclude); err != nil {
			return nil, err
		}
		if r.otherMounts, err = compileAll(p.ID, p.OtherMounts); err != nil {
			return nil, err
		}
		m.rules = append(m.rules, r)
	}
	return m, nil
}

func compileAll(id string, exprs []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(exprs))
	for _, e := range exprs {
		re, err := regexp.Compile(e)
		if err != nil {
			return nil, fmt.Errorf("product %s: %w", id, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Match normalises title and returns which product it belongs to.
func (m *Matcher) Match(title string) Result {
	res := Result{Normalized: Normalize(title)}
	var near []string
	for _, r := range m.rules {
		if ok, _ := r.eval(res.Normalized); ok {
			res.Candidates = append(res.Candidates, r.id)
		} else if r.nearMiss(res.Normalized) {
			near = append(near, r.id)
		}
	}
	switch len(res.Candidates) {
	case 0:
		res.Status = Unmatched
		res.NearMiss = near
	case 1:
		res.Status = Matched
		res.ProductID = res.Candidates[0]
	default:
		res.Status = Ambiguous
	}
	return res
}

// Explain reports, per product, whether title matches and why not.
func (m *Matcher) Explain(title string) (normalized string, traces []Trace) {
	normalized = Normalize(title)
	traces = make([]Trace, 0, len(m.rules))
	for _, r := range m.rules {
		ok, reason := r.eval(normalized)
		traces = append(traces, Trace{ProductID: r.id, Matched: ok, Reason: reason})
	}
	return normalized, traces
}

func (r rule) eval(s string) (bool, string) {
	for _, re := range r.patterns {
		if !re.MatchString(s) {
			return false, fmt.Sprintf("pattern '%s' does not match", re)
		}
	}
	for _, re := range r.requireAll {
		if !re.MatchString(s) {
			return false, fmt.Sprintf("require_all '%s' does not match", re)
		}
	}
	if len(r.requireAny) > 0 && !anyMatch(r.requireAny, s) {
		return false, "no require_any pattern matches"
	}
	for _, re := range r.exclude {
		if re.MatchString(s) {
			return false, fmt.Sprintf("exclude '%s' matches", re)
		}
	}
	return true, "all rules pass"
}

// nearMiss reports whether s matches every pattern and no exclude, but
// fails a require_all or require_any rule, and names none of the product's
// other mounts (that is another mount's version, rejected on purpose).
func (r rule) nearMiss(s string) bool {
	if anyMatch(r.otherMounts, s) {
		return false
	}
	for _, re := range r.patterns {
		if !re.MatchString(s) {
			return false
		}
	}
	for _, re := range r.exclude {
		if re.MatchString(s) {
			return false
		}
	}
	return len(r.requireAll) > 0 || len(r.requireAny) > 0
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
