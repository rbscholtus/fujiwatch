// Package fetch is a polite HTTP client for shop pages: one request at a time
// per host, a delay between requests to the same host, timeouts, and a few
// retries with backoff.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultUserAgent identifies the bot to shop operators.
const DefaultUserAgent = "fujiwatch/0.1 (personal price watcher; +https://github.com/rbscholtus/fujiwatch)"

// maxBodyBytes caps how much of a page is read. Shop pages are well below this.
const maxBodyBytes = 10 << 20

// StatusError is returned for non-200 responses.
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.Code)
}

// retryable reports whether a request that failed with this status might
// succeed if tried again.
func (e *StatusError) retryable() bool {
	return e.Code == http.StatusTooManyRequests || e.Code >= 500
}

// Options configures a Client. Zero values pick sensible defaults.
type Options struct {
	UserAgent string        // default DefaultUserAgent
	Timeout   time.Duration // per attempt; default 20s
	Delay     time.Duration // min gap between requests to one host; default 3s
	Retries   int           // extra attempts after the first; default 2, <0 means none
	Backoff   time.Duration // wait before retry n is Backoff·2^(n-1); default 5s

	Transport http.RoundTripper // for tests; default http.DefaultTransport
}

// Response is a fetched page.
type Response struct {
	URL      string // final URL after redirects
	Status   int
	Header   http.Header
	Body     []byte
	Attempts int
}

// Client fetches pages politely. It is safe for concurrent use; requests to
// the same host are serialised.
type Client struct {
	http    *http.Client
	ua      string
	delay   time.Duration
	retries int
	backoff time.Duration

	mu    sync.Mutex
	hosts map[string]*host
}

type host struct {
	mu   sync.Mutex // held for the whole request, including the polite delay
	last time.Time  // when the previous request to this host finished
}

// New returns a Client.
func New(opts Options) *Client {
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.Delay <= 0 {
		opts.Delay = 3 * time.Second
	}
	switch {
	case opts.Retries == 0:
		opts.Retries = 2
	case opts.Retries < 0:
		opts.Retries = 0
	}
	if opts.Backoff <= 0 {
		opts.Backoff = 5 * time.Second
	}
	return &Client{
		http:    &http.Client{Timeout: opts.Timeout, Transport: opts.Transport},
		ua:      opts.UserAgent,
		delay:   opts.Delay,
		retries: opts.Retries,
		backoff: opts.Backoff,
		hosts:   map[string]*host{},
	}
}

// Get fetches rawURL. It waits for the per-host delay, retries network errors,
// 429 and 5xx responses, and returns a *StatusError for any other non-200.
func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	h := c.host(u.Host)
	h.mu.Lock()
	defer h.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= c.retries+1; attempt++ {
		wait := time.Until(h.last.Add(c.delay))
		if attempt > 1 {
			wait = max(wait, c.backoff<<(attempt-2))
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}

		resp, err := c.do(ctx, rawURL)
		h.last = time.Now()
		if err == nil {
			resp.Attempts = attempt
			return resp, nil
		}
		lastErr = err

		if se, ok := errors.AsType[*StatusError](err); ok && !se.retryable() {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, rawURL string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9,en;q=0.8")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)) // allow connection reuse
		return nil, &StatusError{URL: rawURL, Code: res.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("GET %s: read body: %w", rawURL, err)
	}
	return &Response{
		URL:    res.Request.URL.String(),
		Status: res.StatusCode,
		Header: res.Header,
		Body:   body,
	}, nil
}

func (c *Client) host(name string) *host {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.hosts[name]
	if !ok {
		h = &host{}
		c.hosts[name] = h
	}
	return h
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
