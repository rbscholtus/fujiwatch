package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TelegramURL is the Telegram Bot API.
const TelegramURL = "https://api.telegram.org"

// maxTelegramChars stays under the API's limit of 4096 characters.
const maxTelegramChars = 4000

// Telegram sends one message per run through a bot, with the same content
// as WhatsApp (docs/usage.md, Notifications).
type Telegram struct {
	Token   string
	ChatID  string
	BaseURL string       // default TelegramURL
	Client  *http.Client // default: 30 s timeout
}

// Name returns "telegram".
func (*Telegram) Name() string { return "telegram" }

// telegramMessage is the sendMessage request. There is no parse_mode: shop
// titles contain characters that are special in Markdown and HTML.
type telegramMessage struct {
	ChatID      string              `json:"chat_id"`
	Text        string              `json:"text"`
	LinkPreview telegramLinkPreview `json:"link_preview_options"`
}

type telegramLinkPreview struct {
	IsDisabled bool `json:"is_disabled"`
}

// Send posts the batched alert; with nothing to alert it does nothing.
func (t *Telegram) Send(ctx context.Context, msg *Message) error {
	text := AlertText(msg, maxTelegramChars)
	if text == "" {
		return nil
	}

	base := t.BaseURL
	if base == "" {
		base = TelegramURL
	}
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	body, err := json.Marshal(telegramMessage{ChatID: t.ChatID, Text: text, LinkPreview: telegramLinkPreview{IsDisabled: true}})
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		// The URL carries the token; don't leak it through the error.
		return fmt.Errorf("telegram: %w", redact(err, t.Token))
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: request failed: %w", redact(err, t.Token))
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))

	var reply struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return fmt.Errorf("telegram: HTTP %d: %s", res.StatusCode, firstLine(stripTags(string(raw))))
	}
	if res.StatusCode != http.StatusOK || !reply.OK {
		return fmt.Errorf("telegram: HTTP %d: %s", res.StatusCode, firstLine(reply.Description))
	}
	return nil
}
