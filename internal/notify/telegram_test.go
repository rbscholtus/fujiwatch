package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/diff"
)

const testToken = "123456:secret-token"

func TestTelegramSend(t *testing.T) {
	var paths []string
	var got telegramMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body %s: %v", body, err)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer srv.Close()

	tg := &Telegram{Token: testToken, ChatID: "42", BaseURL: srv.URL}
	if err := tg.Send(t.Context(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if want := "POST /bot" + testToken + "/sendMessage"; len(paths) != 1 || paths[0] != want {
		t.Errorf("requests = %v, want %q", paths, want)
	}
	if got.ChatID != "42" || !got.LinkPreview.IsDisabled || got.Text != AlertText(sampleMessage(), maxTelegramChars) {
		t.Errorf("message = %+v", got)
	}

	// Nothing to alert: no request at all.
	if err := tg.Send(t.Context(), &Message{}); err != nil || len(paths) != 1 {
		t.Errorf("quiet run: err=%v requests=%d", err, len(paths))
	}
}

func TestTelegramSendErrors(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	}))
	defer refused.Close()
	tg := &Telegram{Token: testToken, ChatID: "42", BaseURL: refused.URL}
	if err := tg.Send(t.Context(), sampleMessage()); err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("refused: err = %v", err)
	}

	notOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"description":"nope"}`)
	}))
	defer notOK.Close()
	tg.BaseURL = notOK.URL
	if err := tg.Send(t.Context(), sampleMessage()); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("ok:false with HTTP 200: err = %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>Bad Gateway</html>")
	}))
	defer down.Close()
	tg.BaseURL = down.URL
	if err := tg.Send(t.Context(), sampleMessage()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("502: err = %v", err)
	}

	// Connection failure: the error must not leak the token, which is in
	// the URL's path, not query-escaped.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	tg.BaseURL = closed.URL
	err := tg.Send(t.Context(), sampleMessage())
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("network error = %v (must be non-nil and not contain the token)", err)
	}
}

func TestAlertTextTelegramLimit(t *testing.T) {
	msg := &Message{Names: map[string]string{}}
	for i := range 200 {
		msg.Events = append(msg.Events, diff.Event{Type: diff.NewListing, Product: "p", Shop: "s", Condition: "new", PriceVND: 1_000_000, URL: fmt.Sprintf("https://shop.example/%d/%s", i, strings.Repeat("x", 60))})
	}
	text := AlertText(msg, maxTelegramChars)
	if len(text) > maxTelegramChars || !strings.Contains(text, "more (see email)") {
		t.Errorf("text is %d bytes, want ≤ %d with a remainder line", len(text), maxTelegramChars)
	}
}
