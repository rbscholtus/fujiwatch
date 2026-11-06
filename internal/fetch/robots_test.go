package fetch

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The real vjshop.vn robots.txt (2026-11), plus edge cases.
const vjshopRobots = `User-agent: *
Disallow: /may-anh-cu
Disallow: /ong-kinh-cu
Disallow: /apple-cu
Allow: /
Sitemap: https://vjshop.vn/sitemap.xml`

func TestRobotsVJShop(t *testing.T) {
	r := ParseRobots(vjshopRobots)
	tests := map[string]bool{
		"/do-cu-chat":       true,
		"/may-anh-fujifilm": true,
		"/may-anh-cu":       false,
		"/may-anh-cu/x-t3":  false,
		"/ong-kinh-cu?p=2":  false,
		"/":                 true,
	}
	for path, want := range tests {
		if got := r.Allowed(path); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRobotsRules(t *testing.T) {
	body := `
# comment
User-agent: Googlebot
Disallow: /

User-agent: *
User-agent: other
Disallow: /search
Disallow: /collections/*+*
Disallow: /*.pdf$
Allow: /search/public
Disallow:
`
	r := ParseRobots(body)
	tests := map[string]bool{
		"/products/x-t5":         true,
		"/search":                false,
		"/search?q=x-t5":         false,
		"/search/public":         true, // longer Allow wins
		"/collections/a+b":       false,
		"/collections/fuji":      true,
		"/manual.pdf":            false,
		"/manual.pdf?download=1": true, // $ anchors
		"/products/x-t5.js":      true,
	}
	for path, want := range tests {
		if got := r.Allowed(path); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRobotsOwnGroupWins(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow: /\n\nUser-agent: fujiwatch\nDisallow: /private\n")
	if !r.Allowed("/products/x") || r.Allowed("/private/x") {
		t.Error("fujiwatch group should replace the * group")
	}
	if !ParseRobots("").Allowed("/anything") {
		t.Error("empty robots.txt should allow everything")
	}
	var nilRobots *Robots
	if !nilRobots.Allowed("/x") {
		t.Error("nil robots should allow everything")
	}
}

func TestClientAllowed(t *testing.T) {
	var robotsHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsHits.Add(1)
			_, _ = w.Write([]byte(vjshopRobots))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(Options{Delay: time.Millisecond})
	for _, tt := range []struct {
		path string
		want bool
	}{{"/do-cu-chat", true}, {"/may-anh-cu", false}, {"/ong-kinh-cu?page=2", false}} {
		got, err := c.Allowed(t.Context(), srv.URL+tt.path)
		if err != nil || got != tt.want {
			t.Errorf("Allowed(%s) = %v, %v; want %v", tt.path, got, err, tt.want)
		}
	}
	if n := robotsHits.Load(); n != 1 {
		t.Errorf("robots.txt fetched %d times, want 1 (cached per host)", n)
	}
}

func TestClientAllowedMissingAndBrokenRobots(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	c := New(Options{Delay: time.Millisecond, Backoff: time.Millisecond})
	if ok, err := c.Allowed(t.Context(), missing.URL+"/x"); err != nil || !ok {
		t.Errorf("404 robots.txt: %v, %v; want allowed", ok, err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	if _, err := c.Allowed(t.Context(), broken.URL+"/x"); err == nil {
		t.Error("5xx robots.txt: expected an error")
	}
}
