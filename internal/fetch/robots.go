package fetch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// robotsAgent is the product token matched against User-agent lines.
const robotsAgent = "fujiwatch"

// Robots holds the rules of one robots.txt that apply to us.
type Robots struct {
	rules []robotsRule
}

type robotsRule struct {
	allow bool
	path  string
}

// ParseRobots parses robots.txt, keeping the group for "fujiwatch" if there
// is one, else the "*" group (RFC 9309).
func ParseRobots(body string) *Robots {
	type group struct {
		agents []string
		rules  []robotsRule
	}
	var groups []*group
	var cur *group
	lastWasAgent := false

	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "user-agent":
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(val))
			lastWasAgent = true
		case "allow", "disallow":
			lastWasAgent = false
			if cur == nil || val == "" {
				continue // "Disallow:" with no path allows everything
			}
			cur.rules = append(cur.rules, robotsRule{allow: key == "allow", path: val})
		default:
			lastWasAgent = false
		}
	}

	var star, own []robotsRule
	for _, g := range groups {
		for _, a := range g.agents {
			switch {
			case a == "*":
				star = append(star, g.rules...)
			case strings.Contains(robotsAgent, a) || strings.HasPrefix(a, robotsAgent):
				own = append(own, g.rules...)
			}
		}
	}
	if own != nil {
		return &Robots{rules: own}
	}
	return &Robots{rules: star}
}

// Allowed reports whether path (with query) may be fetched. The longest
// matching rule wins; on a tie, Allow wins.
func (r *Robots) Allowed(pathAndQuery string) bool {
	if r == nil {
		return true
	}
	best, allow := -1, true
	for _, rule := range r.rules {
		if !robotsMatch(rule.path, pathAndQuery) {
			continue
		}
		if n := len(rule.path); n > best || (n == best && rule.allow) {
			best, allow = n, rule.allow
		}
	}
	return allow
}

// robotsMatch matches a robots.txt path pattern: a prefix match where *
// matches any run of characters and a trailing $ anchors the end.
func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	expr := "^" + strings.Join(parts, ".*")
	if anchored {
		expr += "$"
	}
	re, err := regexp.Compile(expr)
	return err == nil && re.MatchString(path)
}

// Allowed reports whether robots.txt of rawURL's host permits fetching it.
// robots.txt is fetched once per host (through the polite client). A missing
// robots.txt (4xx) allows everything; a 5xx or network error is returned so
// the caller can decide.
func (c *Client) Allowed(ctx context.Context, rawURL string) (bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse url: %w", err)
	}

	c.mu.Lock()
	r, cached := c.robots[u.Host]
	c.mu.Unlock()

	if !cached {
		robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
		res, err := c.Get(ctx, robotsURL)
		switch se, ok := errors.AsType[*StatusError](err); {
		case err == nil:
			r = ParseRobots(string(res.Body))
		case ok && se.Code >= 400 && se.Code < 500:
			r = nil // no robots.txt: everything allowed
		default:
			return false, fmt.Errorf("robots.txt: %w", err)
		}
		c.mu.Lock()
		c.robots[u.Host] = r
		c.mu.Unlock()
	}

	pq := u.EscapedPath()
	if pq == "" {
		pq = "/"
	}
	if u.RawQuery != "" {
		pq += "?" + u.RawQuery
	}
	return r.Allowed(pq), nil
}
