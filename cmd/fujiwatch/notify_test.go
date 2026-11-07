package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/notify"
)

type recordingNotifier struct {
	name string
	msgs []*notify.Message
	err  error
}

func (r *recordingNotifier) Name() string { return r.name }
func (r *recordingNotifier) Send(_ context.Context, m *notify.Message) error {
	r.msgs = append(r.msgs, m)
	return r.err
}

func useNotifiers(t *testing.T, email, wa *recordingNotifier, problems ...string) {
	t.Helper()
	old := notifierFactory
	t.Cleanup(func() { notifierFactory = old })
	notifierFactory = func() (notify.Notifier, notify.Notifier, []string) { return email, wa, problems }
}

func TestRunSendsNotifications(t *testing.T) {
	useSnapshotFetcher(t)
	email, wa := &recordingNotifier{name: "email"}, &recordingNotifier{name: "whatsapp"}
	useNotifiers(t, email, wa)

	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--config", "../../config", "--data", t.TempDir()}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if len(email.msgs) != 1 || len(wa.msgs) != 1 {
		t.Fatalf("sent email=%d whatsapp=%d, want 1 each", len(email.msgs), len(wa.msgs))
	}
	m := email.msgs[0]
	if !strings.Contains(m.Subject, "new listing") || len(m.Best) != 2*repoProductCount(t) || len(m.Sources) != repoSourceCount(t) {
		t.Errorf("message: subject %q, best %d, sources %d", m.Subject, len(m.Best), len(m.Sources))
	}
}

func TestRunNoNotifyAndDryRun(t *testing.T) {
	useSnapshotFetcher(t)
	email, wa := &recordingNotifier{name: "email"}, &recordingNotifier{name: "whatsapp"}
	useNotifiers(t, email, wa)

	for _, flag := range []string{"--no-notify", "--dry-run"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"run", flag, "--config", "../../config", "--data", t.TempDir()}, &out, &errOut); code != exitOK {
			t.Fatalf("%s: exit %d: %s", flag, code, errOut.String())
		}
	}
	if len(email.msgs)+len(wa.msgs) != 0 {
		t.Errorf("notifications sent despite --no-notify/--dry-run")
	}
}

// README §8: notification failures are non-fatal; the state is still saved.
func TestRunNotificationFailureIsWarning(t *testing.T) {
	useSnapshotFetcher(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	email := &recordingNotifier{name: "email", err: errors.New("email: 535 bad credentials")}
	wa := &recordingNotifier{name: "whatsapp", err: errors.New("whatsapp: HTTP 502")}
	useNotifiers(t, email, wa, "whatsapp disabled: example problem")

	data := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--config", "../../config", "--data", data}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d, want %d (non-fatal)", code, exitOK)
	}
	for _, want := range []string{
		"::warning::email: 535 bad credentials",
		"::warning::whatsapp: HTTP 502",
		"::warning::whatsapp disabled: example problem",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut.String())
		}
	}
	if _, err := os.Stat(filepath.Join(data, "state.json")); err != nil {
		t.Errorf("state not saved: %v", err)
	}
}

func TestWarnFormat(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	var b bytes.Buffer
	warn(&b, "x")
	if b.String() != "fujiwatch: warning: x\n" {
		t.Errorf("local warn = %q", b.String())
	}
}

// README §13: .env is loaded only when present; real env vars win.
func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	body := `# comment
SMTP_USER=me@gmail.com
export SMTP_PASS="abcd efgh ijkl mnop"
MAIL_TO='a@x.com, b@x.com'
CALLMEBOT_PHONE=+84901 # trailing comment
CALLMEBOT_APIKEY=from-file

EMPTY=
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"SMTP_USER", "SMTP_PASS", "MAIL_TO", "CALLMEBOT_PHONE", "EMPTY"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	t.Setenv("CALLMEBOT_APIKEY", "from-env")

	if err := loadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SMTP_USER":        "me@gmail.com",
		"SMTP_PASS":        "abcd efgh ijkl mnop",
		"MAIL_TO":          "a@x.com, b@x.com",
		"CALLMEBOT_PHONE":  "+84901",
		"CALLMEBOT_APIKEY": "from-env",
		"EMPTY":            "",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	if err := loadDotEnv(filepath.Join(dir, "missing")); err != nil {
		t.Errorf("missing .env: %v", err)
	}
	bad := filepath.Join(dir, "bad.env")
	_ = os.WriteFile(bad, []byte("no equals sign\n"), 0o600)
	if err := loadDotEnv(bad); err == nil || !strings.Contains(err.Error(), ":1:") {
		t.Errorf("bad .env err = %v", err)
	}
}
