package notify

import (
	"fmt"
	"strings"
)

// Environment variables (README §10: repository secrets).
const (
	EnvCallMeBotPhone  = "CALLMEBOT_PHONE"
	EnvCallMeBotAPIKey = "CALLMEBOT_APIKEY" //nolint:gosec // variable name, not a credential
	EnvSMTPUser        = "SMTP_USER"
	EnvSMTPPass        = "SMTP_PASS"
	EnvMailTo          = "MAIL_TO"
	EnvMailFrom        = "MAIL_FROM" // optional; default SMTP_USER
	EnvSMTPAddr        = "SMTP_ADDR" // optional; default smtp.gmail.com:587
)

// DefaultSMTPAddr is Gmail's submission port (STARTTLS).
const DefaultSMTPAddr = "smtp.gmail.com:587"

// FromEnv builds the notifiers whose credentials are set. A notifier with
// only some of its variables set is reported as a problem; one with none is
// silently skipped.
func FromEnv(getenv func(string) string) (email *Email, whatsapp *WhatsApp, problems []string) {
	if v := present(getenv, EnvSMTPUser, EnvSMTPPass, EnvMailTo); len(v) == 3 {
		var to []string
		for a := range strings.SplitSeq(getenv(EnvMailTo), ",") {
			if a = strings.TrimSpace(a); a != "" {
				to = append(to, a)
			}
		}
		email = &Email{
			Addr: or(getenv(EnvSMTPAddr), DefaultSMTPAddr),
			User: getenv(EnvSMTPUser),
			Pass: getenv(EnvSMTPPass),
			From: or(getenv(EnvMailFrom), getenv(EnvSMTPUser)),
			To:   to,
		}
	} else if len(v) > 0 {
		problems = append(problems, fmt.Sprintf("email disabled: set %s, %s and %s (only %s set)",
			EnvSMTPUser, EnvSMTPPass, EnvMailTo, strings.Join(v, ", ")))
	}

	if v := present(getenv, EnvCallMeBotPhone, EnvCallMeBotAPIKey); len(v) == 2 {
		whatsapp = &WhatsApp{Phone: getenv(EnvCallMeBotPhone), APIKey: getenv(EnvCallMeBotAPIKey)}
	} else if len(v) > 0 {
		problems = append(problems, fmt.Sprintf("whatsapp disabled: set %s and %s (only %s set)",
			EnvCallMeBotPhone, EnvCallMeBotAPIKey, strings.Join(v, ", ")))
	}
	return email, whatsapp, problems
}

func present(getenv func(string) string, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if strings.TrimSpace(getenv(k)) != "" {
			out = append(out, k)
		}
	}
	return out
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
