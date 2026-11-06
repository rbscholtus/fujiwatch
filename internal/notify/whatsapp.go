package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/price"
)

// CallMeBotURL is the CallMeBot WhatsApp endpoint.
const CallMeBotURL = "https://api.callmebot.com/whatsapp.php"

// maxWhatsAppChars keeps the message short; CallMeBot is a free shared
// service and the text travels in the URL.
const maxWhatsAppChars = 1000

// WhatsApp sends one short message per run via CallMeBot, only for
// price_drop, back_in_stock and new_listing (README §8).
type WhatsApp struct {
	Phone   string // international format, e.g. +84…
	APIKey  string
	BaseURL string       // default CallMeBotURL
	Client  *http.Client // default: 30 s timeout
}

// Name returns "whatsapp".
func (*WhatsApp) Name() string { return "whatsapp" }

// Send posts the batched alert; with nothing to alert it does nothing.
func (w *WhatsApp) Send(ctx context.Context, msg *Message) error {
	text := WhatsAppText(msg)
	if text == "" {
		return nil
	}

	base := w.BaseURL
	if base == "" {
		base = CallMeBotURL
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	q := url.Values{"phone": {w.Phone}, "text": {text}, "apikey": {w.APIKey}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("whatsapp: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		// The URL carries the API key; don't leak it through the error.
		return fmt.Errorf("whatsapp: request failed: %w", redact(err, w.APIKey))
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))

	// CallMeBot answers 200 with an HTML page even when the key is wrong.
	if res.StatusCode != http.StatusOK || strings.Contains(strings.ToLower(string(body)), "apikey is invalid") {
		return fmt.Errorf("whatsapp: HTTP %d: %s", res.StatusCode, firstLine(stripTags(string(body))))
	}
	return nil
}

// WhatsAppText renders the instant alert, or "" if nothing qualifies.
func WhatsAppText(msg *Message) string {
	events := instant(msg.Events)
	if len(events) == 0 {
		return ""
	}

	var lines []string
	for _, e := range events {
		name := msg.Names[e.Product]
		if name == "" {
			name = e.Product
		}
		var line string
		switch e.Type {
		case diff.PriceDrop:
			pct := float64(e.OldPrice-e.PriceVND) * 100 / float64(e.OldPrice)
			line = fmt.Sprintf("📉 %s (%s) %s: %s → %s (-%.0f%%)", name, e.Condition, e.Shop,
				price.FormatVND(e.OldPrice), price.FormatVND(e.PriceVND), pct)
		case diff.BackInStock:
			line = fmt.Sprintf("✅ %s (%s) back in stock at %s: %s", name, e.Condition, e.Shop, price.FormatVND(e.PriceVND))
		case diff.NewListing:
			line = fmt.Sprintf("🆕 %s (%s) %s: %s", name, e.Condition, e.Shop, price.FormatVND(e.PriceVND))
		case diff.PriceRise, diff.OutOfStock, diff.Gone, diff.AdapterBroken:
			continue
		default:
			continue
		}
		lines = append(lines, line+"\n"+e.URL)
	}

	header := fmt.Sprintf("fujiwatch: %d alert", len(lines))
	if len(lines) != 1 {
		header += "s"
	}
	const reserve = 40 // room for the "…and N more" line
	text := header
	for i, l := range lines {
		if len(text)+2+len(l)+reserve > maxWhatsAppChars {
			return text + fmt.Sprintf("\n\n…and %d more (see email)", len(lines)-i)
		}
		text += "\n\n" + l
	}
	return text
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func redact(err error, secret string) error {
	if secret == "" {
		return err
	}
	return &redactedError{
		msg: strings.ReplaceAll(err.Error(), url.QueryEscape(secret), "REDACTED"),
		err: err,
	}
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
			b.WriteByte(' ')
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func firstLine(s string) string {
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
