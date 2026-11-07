package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastClient() *Client {
	return New(Options{Delay: time.Millisecond, Backoff: time.Millisecond, Timeout: 2 * time.Second})
}

func TestGetOK(t *testing.T) {
	var gotUA, gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.UserAgent()
		gotLang = r.Header.Get("Accept-Language")
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	res, err := fastClient().Get(t.Context(), srv.URL+"/p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(res.Body) != "<html>ok</html>" || res.Status != http.StatusOK || res.Attempts != 1 {
		t.Errorf("unexpected response: %+v", res)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotLang == "" {
		t.Error("Accept-Language not set")
	}
}

func TestGetRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res, err := fastClient().Get(t.Context(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", res.Attempts)
	}
}

func TestGetGivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := fastClient().Get(t.Context(), srv.URL)
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.Code != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want StatusError 429", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (1 + 2 retries)", got)
	}
}

func TestGetDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fastClient().Get(t.Context(), srv.URL)
	if se, ok := errors.AsType[*StatusError](err); !ok || se.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want StatusError 404", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestGetNoRetriesOption(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(Options{Delay: time.Millisecond, Retries: -1})
	if _, err := c.Get(t.Context(), srv.URL); err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestGetPoliteDelayPerHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	const delay = 150 * time.Millisecond
	c := New(Options{Delay: delay})

	if _, err := c.Get(t.Context(), srv.URL+"/a"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := c.Get(t.Context(), srv.URL+"/b"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < delay {
		t.Errorf("second request to same host after %v, want ≥ %v", elapsed, delay)
	}
}

func TestGetContextCancelledDuringDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New(Options{Delay: time.Hour})
	if _, err := c.Get(t.Context(), srv.URL); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestPostForm(t *testing.T) {
	var method, ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ctype = r.Method, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res, err := fastClient().PostForm(t.Context(), srv.URL+"/more", url.Values{"paging": {"0"}, "pageSize": {"300"}})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || ctype != "application/x-www-form-urlencoded" || body != "pageSize=300&paging=0" || string(res.Body) != `{"ok":true}` {
		t.Errorf("method %s, content type %q, body %q, response %q", method, ctype, body, res.Body)
	}

	_, err = fastClient().PostForm(t.Context(), srv.URL+"/missing", url.Values{})
	if err == nil || !strings.HasPrefix(err.Error(), "POST ") {
		t.Errorf("error = %v, want a POST status error", err)
	}
}
