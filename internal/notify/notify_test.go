package notify

// Spec tests for docs/usage.md, Notifications.

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/runner"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

var now = time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC) // Sunday 06:17 in Vietnam

func sampleMessage() *Message {
	return &Message{
		Subject: "fujiwatch: 1 price drop, 1 new listing, 1 broken source",
		Time:    now,
		Events: []diff.Event{
			{Type: diff.PriceDrop, Shop: "xspace", URL: "https://x/p", Product: "xf10-24-ii", Title: "XF10-24 WR II",
				Condition: "new", OldPrice: 27_490_000, PriceVND: 25_990_000},
			{Type: diff.NewListing, Shop: "vjshop", URL: "https://v/x-t3", Product: "x-t3", Title: "X-T3 <b>cũ</b> 98%",
				Condition: "used", PriceVND: 15_500_000},
			{Type: diff.PriceRise, Shop: "vjshop", URL: "https://v/x-t5", Product: "x-t5", Condition: "new",
				OldPrice: 36_990_000, PriceVND: 37_990_000},
			{Type: diff.AdapterBroken, Shop: "kyma", URL: "https://kyma.vn/", Detail: "GET https://kyma.vn/: HTTP 503"},
		},
		Best: []Best{
			{Product: "xf10-24-ii", Name: "XF10-24mm F4 R OIS WR (II)", Condition: "new", Count: 1,
				Offer: &state.Offer{Shop: "xspace", URL: "https://x/p", PriceVND: 25_990_000}},
			{Product: "xf10-24-ii", Name: "XF10-24mm F4 R OIS WR (II)", Condition: "used"},
		},
		Sources: []runner.SourceReport{
			{Shop: "xspace", Mode: "product", URL: "https://x/p", Offers: 1, Matched: 1},
			{Shop: "kyma", Mode: "listing", URL: "https://kyma.vn/", Err: "GET https://kyma.vn/: HTTP 503"},
		},
		Unmatched: []runner.Unmatched{
			{Shop: "vjshop", URL: "https://v/x-e3", Title: "Fujifilm X-E3", Status: "unmatched"},
			{Shop: "kyma", URL: "https://k/x-h2", Title: "Fujifilm X-H2", Status: "unmatched"},
			{Shop: "vjshop", URL: "https://v/x-s20", Title: "Fujifilm X-S20", Status: "unmatched"},
			{Shop: "vjshop", URL: "https://v/tamron", Title: "Tamron 16-50mm F2.8 Di III VXD", Status: "unmatched", NearMiss: []string{"tamron-16-50"}},
			{Shop: "vjshop", URL: "https://v/combo", Title: "Combo X-T4 + XF10-24 WR", Status: "ambiguous", Candidates: []string{"x-t4", "xf10-24-ii"}},
		},
		Names: map[string]string{"xf10-24-ii": "XF10-24mm F4 R OIS WR (II)", "x-t3": "Fujifilm X-T3", "x-t5": "Fujifilm X-T5"},
	}
}

func TestSubject(t *testing.T) {
	ev := func(ts ...diff.Type) []diff.Event {
		out := make([]diff.Event, 0, len(ts))
		for _, t := range ts {
			out = append(out, diff.Event{Type: t})
		}
		return out
	}
	tests := []struct {
		events []diff.Event
		want   string
	}{
		{nil, "fujiwatch: no changes"},
		{ev(diff.PriceDrop), "fujiwatch: 1 price drop"},
		{ev(diff.NewListing, diff.PriceDrop, diff.PriceDrop), "fujiwatch: 2 price drops, 1 new listing"},
		{ev(diff.PriceRise, diff.AdapterBroken, diff.Gone), "fujiwatch: 1 broken source, 1 gone, 1 price rise"},
	}
	for _, tt := range tests {
		if got := Subject(tt.events); got != tt.want {
			t.Errorf("Subject = %q, want %q", got, tt.want)
		}
	}
}

// docs/usage.md, Notifications: "a table of the current best price per product (new vs used)".
func TestBestPrices(t *testing.T) {
	st := state.New()
	add := func(url, product, cond string, p int64, stock *bool) {
		st.Offers["s|"+url] = &state.Offer{Product: product, Shop: "s", URL: url, Condition: cond, PriceVND: p, InStock: stock}
	}
	add("a", "x-t5", "new", 37_000_000, new(true))
	add("b", "x-t5", "new", 35_000_000, new(false)) // out of stock: skipped
	add("c", "x-t5", "new", 0, new(true))           // no price: skipped
	add("d", "x-t5", "new", 36_500_000, nil)        // unknown stock: allowed
	add("e", "x-t5", "used", 25_000_000, nil)
	add("f", "x-t3", "used", 15_000_000, new(true))

	products := []config.Product{{ID: "x-t3", Name: "X-T3"}, {ID: "x-t5", Name: "X-T5"}, {ID: "x-t4", Name: "X-T4"}}
	got := BestPrices(products, st)
	if len(got) != 6 {
		t.Fatalf("got %d rows, want 6 (3 products × new/used)", len(got))
	}
	type row struct {
		id, cond string
		url      string
		count    int
	}
	want := []row{
		{"x-t3", "new", "", 0}, {"x-t3", "used", "f", 1},
		{"x-t5", "new", "d", 4}, {"x-t5", "used", "e", 1},
		{"x-t4", "new", "", 0}, {"x-t4", "used", "", 0},
	}
	for i, w := range want {
		b := got[i]
		url := ""
		if b.Offer != nil {
			url = b.Offer.URL
		}
		if b.Product != w.id || b.Condition != w.cond || url != w.url || b.Count != w.count {
			t.Errorf("row %d = %s/%s best=%q count=%d, want %+v", i, b.Product, b.Condition, url, b.Count, w)
		}
	}
}

// docs/usage.md, Notifications: email only on events, plus a weekly heartbeat.
func TestShouldEmail(t *testing.T) {
	events := []diff.Event{{Type: diff.PriceRise}}
	saturdayICT := now.Add(-24 * time.Hour)
	tests := []struct {
		name   string
		n      config.Notify
		events []diff.Event
		at     time.Time
		want   bool
	}{
		{"default always", config.Notify{}, nil, now, true},
		{"always", config.Notify{Email: "always"}, nil, saturdayICT, true},
		{"changes with events", config.Notify{Email: "changes"}, events, saturdayICT, true},
		{"changes, quiet, no heartbeat", config.Notify{Email: "changes"}, nil, now, false},
		{"changes, quiet, heartbeat day (ICT)", config.Notify{Email: "changes", Heartbeat: "sunday"}, nil, now, true},
		{"changes, quiet, other day", config.Notify{Email: "changes", Heartbeat: "Sunday"}, nil, saturdayICT, false},
	}
	for _, tt := range tests {
		if got := ShouldEmail(tt.n, tt.events, tt.at); got != tt.want {
			t.Errorf("%s: ShouldEmail = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	cfg := &config.Config{Watchlist: config.Watchlist{Products: []config.Product{{ID: "x-t5", Name: "Fujifilm X-T5"}}}}
	rep := &runner.Report{Time: now, Events: []diff.Event{{Type: diff.NewListing, Product: "x-t5"}}}
	msg, err := BuildMessage(cfg, rep, state.New(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "fujiwatch: 1 new listing" || msg.Names["x-t5"] != "Fujifilm X-T5" || len(msg.Best) != 2 || !msg.Time.Equal(now) {
		t.Errorf("message = %+v", msg)
	}
}

// ── WhatsApp ─────────────────────────────────────────────────────

// docs/usage.md, Notifications: only price_drop, back_in_stock and new_listing, batched into
// one short message.
func TestWhatsAppText(t *testing.T) {
	text := AlertText(sampleMessage(), maxWhatsAppChars)
	for _, want := range []string{
		"fujiwatch: 2 alerts",
		"📉 XF10-24mm F4 R OIS WR (II) (new) xspace: 27.490.000₫ → 25.990.000₫ (-5%)",
		"🆕 Fujifilm X-T3 (used) vjshop: 15.500.000₫",
		"https://v/x-t3",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"x-t5", "kyma", "37.990.000"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("text should not mention %q (not an instant event):\n%s", unwanted, text)
		}
	}

	quiet := &Message{Events: []diff.Event{{Type: diff.PriceRise}, {Type: diff.Gone}, {Type: diff.AdapterBroken}, {Type: diff.OutOfStock}}}
	if got := AlertText(quiet, maxWhatsAppChars); got != "" {
		t.Errorf("no instant events: text = %q, want empty", got)
	}

	back := &Message{Events: []diff.Event{{Type: diff.BackInStock, Product: "p", Shop: "s", Condition: "new", PriceVND: 1_000_000, URL: "u"}}}
	if got := AlertText(back, maxWhatsAppChars); !strings.Contains(got, "1 alert\n") || !strings.Contains(got, "✅ p (new) back in stock at s: 1.000.000₫") {
		t.Errorf("back in stock text = %q", got)
	}
}

// Events from a source's first run are a baseline: in the email, with a
// note, but never on WhatsApp.
func TestBaselineEventsAreEmailOnly(t *testing.T) {
	msg := sampleMessage()
	for i := range msg.Events {
		msg.Events[i].Baseline = true
	}
	if got := AlertText(msg, maxWhatsAppChars); got != "" {
		t.Errorf("baseline events sent to WhatsApp:\n%s", got)
	}
	msg.Events = append(msg.Events, diff.Event{Type: diff.PriceDrop, Product: "x-t5", Shop: "kyma", Condition: "new", OldPrice: 40_000_000, PriceVND: 38_000_000, URL: "https://k/x-t5"})
	if got := AlertText(msg, maxWhatsAppChars); !strings.Contains(got, "1 alert\n") || !strings.Contains(got, "https://k/x-t5") {
		t.Errorf("non-baseline event not sent:\n%s", got)
	}

	raw, err := (&Email{From: "f@x", To: []string{"t@x"}}).Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	_, _, text, html := parseEmail(t, raw)
	n := len(msg.Events) - 1
	for _, part := range []string{text, html} {
		if !strings.Contains(part, fmt.Sprintf("%d from a source's first run", n)) {
			t.Errorf("email lacks the baseline note")
		}
	}
}

func TestWhatsAppTextTruncates(t *testing.T) {
	msg := &Message{}
	for i := range 60 {
		msg.Events = append(msg.Events, diff.Event{Type: diff.NewListing, Product: "x-t3", Shop: "vjshop",
			Condition: "used", PriceVND: 15_000_000, URL: fmt.Sprintf("https://vjshop.vn/may-anh-fujifilm-x-t3-body-2nd-%06d", i)})
	}
	text := AlertText(msg, maxWhatsAppChars)
	if len(text) > maxWhatsAppChars {
		t.Errorf("text is %d bytes, want ≤ %d", len(text), maxWhatsAppChars)
	}
	if !strings.Contains(text, "fujiwatch: 60 alerts") || !strings.Contains(text, "more (see email)") {
		t.Errorf("truncated text:\n%s", text)
	}
}

func TestWhatsAppSend(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got = append(got, q.Get("phone")+"|"+q.Get("apikey")+"|"+q.Get("text"))
		_, _ = io.WriteString(w, "<p>Message queued. You will receive it in a few seconds.</p>")
	}))
	defer srv.Close()

	w := &WhatsApp{Phone: "+84901234567", APIKey: "123456", BaseURL: srv.URL}
	if err := w.Send(t.Context(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("requests = %d, want exactly 1 per run", len(got))
	}
	if !strings.HasPrefix(got[0], "+84901234567|123456|fujiwatch: 2 alerts") {
		t.Errorf("request = %q", got[0])
	}

	// Nothing to alert: no request at all.
	if err := w.Send(t.Context(), &Message{}); err != nil || len(got) != 1 {
		t.Errorf("quiet run: err=%v requests=%d", err, len(got))
	}
}

func TestWhatsAppSendErrors(t *testing.T) {
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html><b>APIKey is invalid.</b> Please check.</html>")
	}))
	defer invalid.Close()
	w := &WhatsApp{Phone: "+84", APIKey: "secret-key", BaseURL: invalid.URL}
	if err := w.Send(t.Context(), sampleMessage()); err == nil || !strings.Contains(err.Error(), "APIKey is invalid") {
		t.Errorf("invalid key: err = %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer down.Close()
	w.BaseURL = down.URL
	if err := w.Send(t.Context(), sampleMessage()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("502: err = %v", err)
	}

	// Connection failure: the error must not leak the API key.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	w.BaseURL = closed.URL
	err := w.Send(t.Context(), sampleMessage())
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Errorf("network error = %v (must be non-nil and not contain the key)", err)
	}
}

// ── Email ────────────────────────────────────────────────────────

// parseEmail returns the decoded subject and the text and html parts.
func parseEmail(t *testing.T, raw []byte) (*mail.Message, string, string, string) {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("content type %q, %v", mt, err)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	parts := map[string]string{}
	for {
		p, err := mr.NextPart() // decodes quoted-printable transparently
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		parts[ct] = string(b)
	}
	return m, subject, parts["text/plain"], parts["text/html"]
}

// docs/usage.md, Notifications: digest contents: events at the top, best prices, unmatched or
// ambiguous offers, and adapter health.
func TestEmailRender(t *testing.T) {
	e := &Email{From: "bot@example.com", To: []string{"a@example.com", "b@example.com"}}
	msg := sampleMessage()
	msg.Subject = "fujiwatch: 1 price drop — ống kính"
	raw, err := e.Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	m, subject, text, html := parseEmail(t, raw)

	if subject != msg.Subject {
		t.Errorf("subject = %q", subject)
	}
	if m.Header.Get("To") != "a@example.com, b@example.com" || m.Header.Get("From") != "bot@example.com" {
		t.Errorf("headers = %v", m.Header)
	}
	if _, err := m.Header.Date(); err != nil {
		t.Errorf("Date header: %v", err)
	}
	if m.Header.Get("Message-Id") == "" {
		t.Error("missing Message-ID")
	}

	for _, want := range []string{
		"EVENTS (4)",
		"PRICE DROP: XF10-24mm F4 R OIS WR (II) (new) at xspace: 27.490.000₫ → 25.990.000₫ (-5.5%)",
		"NEW LISTING: Fujifilm X-T3 (used) at vjshop: 15.500.000₫",
		"BROKEN: kyma: GET https://kyma.vn/: HTTP 503",
		"BEST PRICES",
		"XF10-24mm F4 R OIS WR (II) (new): 25.990.000₫ at xspace https://x/p [1 known]",
		"XF10-24mm F4 R OIS WR (II) (used): – [0 known]",
		"SOURCES (2, 1 failing)",
		"FAIL kyma listing",
		"ambiguous (1)",
		"Combo X-T4 + XF10-24 WR [x-t4 xf10-24-ii]",
		"near miss (1)",
		"Tamron 16-50mm F2.8 Di III VXD [tamron-16-50]",
		"Other unmatched offers: 3 (vjshop 2, kyma 1)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text part missing %q", want)
		}
	}
	// Events come first, ambiguous before near misses before the count.
	if strings.Index(text, "EVENTS") > strings.Index(text, "BEST PRICES") ||
		strings.Index(text, "ambiguous (1)") > strings.Index(text, "near miss (1)") ||
		strings.Index(text, "near miss (1)") > strings.Index(text, "Other unmatched") {
		t.Error("section order wrong")
	}
	// Plain unmatched offers are counted, not listed.
	for _, unwanted := range []string{"Fujifilm X-E3", "X-S20", "https://k/x-h2"} {
		if strings.Contains(text, unwanted) || strings.Contains(html, unwanted) {
			t.Errorf("email lists plain unmatched offer %q", unwanted)
		}
	}
	if strings.Contains(text, "first run") {
		t.Error("baseline note without baseline events")
	}

	for _, want := range []string{"<h3>Events (4)</h3>", `<a href="https://x/p">25.990.000₫</a>`, "FAIL", "X-T3 &lt;b&gt;cũ&lt;/b&gt; 98%"} {
		if !strings.Contains(html, want) {
			t.Errorf("html part missing %q", want)
		}
	}
	if strings.Contains(html, "<b>cũ</b>") {
		t.Error("shop titles must be HTML-escaped")
	}
}

func TestEmailRenderNoChanges(t *testing.T) {
	raw, err := (&Email{From: "f@x", To: []string{"t@x"}}).Render(&Message{Subject: Subject(nil), Time: now})
	if err != nil {
		t.Fatal(err)
	}
	_, subject, text, _ := parseEmail(t, raw)
	if subject != "fujiwatch: no changes" || !strings.Contains(text, "No changes.") {
		t.Errorf("subject %q, text:\n%s", subject, text)
	}
}

// The SMTP password must only ever be sent over TLS (Gmail STARTTLS).
func TestEmailUsesPlainAuthForHost(t *testing.T) {
	var auth smtp.Auth
	e := &Email{Addr: "smtp.gmail.com:587", User: "u", Pass: "p", From: "u", To: []string{"t"},
		send: func(_ string, a smtp.Auth, _ string, _ []string, _ []byte) error { auth = a; return nil }}
	if err := e.Send(t.Context(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: false, Auth: []string{"PLAIN"}}); err == nil {
		t.Error("auth over plaintext to a remote host must be refused")
	}
	if _, _, err := auth.Start(&smtp.ServerInfo{Name: "smtp.gmail.com", TLS: true, Auth: []string{"PLAIN"}}); err != nil {
		t.Errorf("auth over TLS: %v", err)
	}
	if err := (&Email{Addr: "no-port"}).Send(t.Context(), sampleMessage()); err == nil {
		t.Error("bad address: expected error")
	}
}

// fakeSMTP is a minimal SMTP server for one session on 127.0.0.1.
type fakeSMTP struct {
	addr string
	mu   sync.Mutex
	auth string
	from string
	rcpt []string
	data string
	done chan struct{}
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String(), done: make(chan struct{})}
	go func() {
		defer close(f.done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 localhost ESMTP fake")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.Fields(line + " x")[0])
			f.mu.Lock()
			switch cmd {
			case "EHLO", "HELO":
				_ = tp.PrintfLine("250-localhost")
				_ = tp.PrintfLine("250 AUTH PLAIN")
			case "AUTH":
				f.auth = line
				_ = tp.PrintfLine("235 2.7.0 Accepted")
			case "MAIL":
				f.from = line
				_ = tp.PrintfLine("250 OK")
			case "RCPT":
				f.rcpt = append(f.rcpt, line)
				_ = tp.PrintfLine("250 OK")
			case "DATA":
				_ = tp.PrintfLine("354 go ahead")
				b, _ := tp.ReadDotBytes()
				f.data = string(b)
				_ = tp.PrintfLine("250 queued")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				f.mu.Unlock()
				return
			default:
				_ = tp.PrintfLine("250 OK")
			}
			f.mu.Unlock()
		}
	}()
	return f
}

func TestEmailSendOverSMTP(t *testing.T) {
	srv := startFakeSMTP(t)
	e := &Email{Addr: srv.addr, User: "me@gmail.com", Pass: "app-password", From: "me@gmail.com",
		To: []string{"a@example.com", "b@example.com"}}
	if err := e.Send(t.Context(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.done:
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP session did not finish")
	}

	creds, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(srv.auth, "AUTH PLAIN "))
	if string(creds) != "\x00me@gmail.com\x00app-password" {
		t.Errorf("AUTH = %q", creds)
	}
	if srv.from != "MAIL FROM:<me@gmail.com>" && !strings.HasPrefix(srv.from, "MAIL FROM:<me@gmail.com>") {
		t.Errorf("MAIL = %q", srv.from)
	}
	if len(srv.rcpt) != 2 || !strings.Contains(srv.rcpt[1], "b@example.com") {
		t.Errorf("RCPT = %v", srv.rcpt)
	}
	sc := bufio.NewScanner(strings.NewReader(srv.data))
	found := false
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "Subject: ") {
			found = true
		}
	}
	if !found {
		t.Errorf("DATA has no Subject header:\n%.400s", srv.data)
	}
}

func TestEmailSendCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (&Email{Addr: "127.0.0.1:1"}).Send(ctx, sampleMessage()); err == nil {
		t.Error("expected error for cancelled context")
	}
}

// ── environment ──────────────────────────────────────────────────

func TestFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	email, instant, problems := FromEnv(env(map[string]string{
		EnvSMTPUser: "me@gmail.com", EnvSMTPPass: "pw", EnvMailTo: "a@x.com, b@x.com ,",
		EnvCallMeBotPhone: "+84901", EnvCallMeBotAPIKey: "k",
		EnvTelegramToken: "1:t", EnvTelegramChatID: "42",
	}))
	if email == nil || len(instant) != 2 || len(problems) != 0 {
		t.Fatalf("email=%v instant=%v problems=%v", email, instant, problems)
	}
	if email.Addr != DefaultSMTPAddr || email.From != "me@gmail.com" || len(email.To) != 2 || email.To[1] != "b@x.com" {
		t.Errorf("email = %+v", email)
	}
	if wa, ok := instant[0].(*WhatsApp); !ok || wa.Phone != "+84901" || wa.APIKey != "k" {
		t.Errorf("whatsapp = %+v", instant[0])
	}
	if tg, ok := instant[1].(*Telegram); !ok || tg.Token != "1:t" || tg.ChatID != "42" {
		t.Errorf("telegram = %+v", instant[1])
	}

	email, _, _ = FromEnv(env(map[string]string{
		EnvSMTPUser: "u", EnvSMTPPass: "p", EnvMailTo: "t", EnvMailFrom: "Bot <bot@x>", EnvSMTPAddr: "smtp.x:2525",
	}))
	if email.From != "Bot <bot@x>" || email.Addr != "smtp.x:2525" {
		t.Errorf("overrides not applied: %+v", email)
	}

	email, instant, problems = FromEnv(env(nil))
	if email != nil || len(instant) != 0 || len(problems) != 0 {
		t.Errorf("nothing set: email=%v instant=%v problems=%v", email, instant, problems)
	}

	email, instant, problems = FromEnv(env(map[string]string{EnvSMTPUser: "u", EnvCallMeBotAPIKey: "k", EnvTelegramChatID: "42"}))
	if email != nil || len(instant) != 0 || len(problems) != 3 {
		t.Errorf("partial: email=%v instant=%v problems=%v", email, instant, problems)
	}
}

// A flash sale is an instant alert, with its window (Vietnam time).
func TestFlashSaleAlert(t *testing.T) {
	start := time.Date(2026, 11, 8, 13, 0, 0, 0, time.UTC) // Sun 20:00 ICT
	e := diff.Event{Type: diff.FlashSale, Product: "x-t5", Shop: "bncamera", Condition: "new", URL: "https://bncamera.com/x",
		PriceVND: 33_900_000, OldPrice: 38_490_000, Start: start, End: start.Add(2 * time.Hour), Quota: "1/5"}
	msg := &Message{Events: []diff.Event{e}, Names: map[string]string{"x-t5": "Fujifilm X-T5"}}
	text := AlertText(msg, maxWhatsAppChars)
	for _, want := range []string{"1 alert", "⚡ Fujifilm X-T5 (new) bncamera flash sale: 33.900.000₫ (normally 38.490.000₫, -12%), Sun 08/11 20:00–22:00, 1/5 sold", "https://bncamera.com/x"} {
		if !strings.Contains(text, want) {
			t.Errorf("WhatsApp text missing %q:\n%s", want, text)
		}
	}
	if s := Subject(msg.Events); s != "fujiwatch: 1 flash sale" {
		t.Errorf("subject = %q", s)
	}
}
