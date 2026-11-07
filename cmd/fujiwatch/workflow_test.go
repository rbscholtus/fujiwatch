package main

// Spec tests for docs/usage.md, GitHub Actions. Workflows are checked as text
// (standard library only) and their shell steps are executed for real.

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/notify"
)

const workflowDir = "../../.github/workflows"

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(workflowDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\t") {
		t.Fatalf("%s contains tabs (invalid YAML indentation)", name)
	}
	return string(b)
}

// stepScript returns the `run: |` block of the step with the given name.
func stepScript(t *testing.T, wf, step string) string {
	t.Helper()
	lines := strings.Split(wf, "\n")
	inStep := false
	for i, l := range lines {
		if strings.TrimSpace(l) == "- name: "+step {
			inStep = true
			continue
		}
		if !inStep || strings.TrimSpace(l) != "run: |" {
			if inStep && strings.HasPrefix(strings.TrimSpace(l), "- ") {
				break // next step without a script
			}
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		var b strings.Builder
		for _, s := range lines[i+1:] {
			if strings.TrimSpace(s) != "" && len(s)-len(strings.TrimLeft(s, " ")) <= indent {
				break
			}
			if len(s) > indent+2 {
				s = s[indent+2:]
			} else {
				s = strings.TrimSpace(s)
			}
			b.WriteString(s + "\n")
		}
		return b.String()
	}
	t.Fatalf("step %q with a run block not found", step)
	return ""
}

func TestWatchWorkflowSchedule(t *testing.T) {
	wf := readWorkflow(t, "watch.yml")

	m := regexp.MustCompile(`cron:\s*"(\d+) (\d+) \* \* \*"`).FindStringSubmatch(wf)
	if m == nil {
		t.Fatal("no daily cron schedule")
	}
	at, err := time.Parse("15:04", m[2]+":"+m[1])
	if err != nil {
		t.Fatal(err)
	}
	if ict := at.In(time.FixedZone("ICT", 7*3600)); ict.Format("15:04") != "06:17" {
		t.Errorf("cron runs at %s Vietnam time, want 06:17", ict.Format("15:04"))
	}
	if at.Minute() == 0 {
		t.Error("cron should be off the hour to avoid queue delays")
	}

	for _, want := range []string{
		"workflow_dispatch:",
		"contents: write",
		"group: watch",
		"cancel-in-progress: false",
		"go-version-file: go.mod",
		"go run ./cmd/fujiwatch",
		"git add data/;",
		"[skip ci]",
		"git diff --cached --quiet",
		"if: ${{ !inputs.dry_run }}",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("watch.yml missing %q", want)
		}
	}

	// Tests run before the real run, so a broken build never touches data.
	if strings.Index(wf, "go test ./...") > strings.Index(wf, "go run ./cmd/fujiwatch") {
		t.Error("go test must run before the watch run")
	}
}

// Every variable the notifiers read must be passed from a secret, and
// documented in docs/usage.md.
func TestWatchWorkflowSecrets(t *testing.T) {
	wf := readWorkflow(t, "watch.yml")
	readme, err := os.ReadFile("../../docs/usage.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		notify.EnvCallMeBotPhone, notify.EnvCallMeBotAPIKey,
		notify.EnvSMTPUser, notify.EnvSMTPPass, notify.EnvMailTo,
	} {
		re := regexp.MustCompile(`(?m)^\s+` + name + `:\s+\$\{\{ secrets\.` + name + ` \}\}$`)
		if !re.MatchString(wf) {
			t.Errorf("watch.yml does not pass secret %s", name)
		}
		if !strings.Contains(string(readme), "`"+name+"`") {
			t.Errorf("docs/usage.md does not document secret %s", name)
		}
	}
}

func TestWorkflowActionsPinnedToMajor(t *testing.T) {
	for _, name := range []string{"watch.yml", "ci.yml"} {
		wf := readWorkflow(t, name)
		uses := regexp.MustCompile(`uses:\s*(\S+)`).FindAllStringSubmatch(wf, -1)
		if len(uses) == 0 {
			t.Fatalf("%s uses no actions", name)
		}
		for _, u := range uses {
			if !regexp.MustCompile(`^[\w.-]+/[\w.-]+@v\d+$`).MatchString(u[1]) {
				t.Errorf("%s: %s should be pinned to a major version tag", name, u[1])
			}
		}
	}
}

func TestCIWorkflow(t *testing.T) {
	wf := readWorkflow(t, "ci.yml")
	for _, want := range []string{"pull_request:", "go vet ./...", "go test -race ./...", "golangci/golangci-lint-action@", `"data/**"`} {
		if !strings.Contains(wf, want) {
			t.Errorf("ci.yml missing %q", want)
		}
	}
	// The lint action must run golangci-lint v2, matching .golangci.yml.
	cfg, err := os.ReadFile("../../.golangci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `version: "2"`) || !regexp.MustCompile(`version: v2\.\d+`).MatchString(wf) {
		t.Error("golangci-lint version in ci.yml must be v2.x to match .golangci.yml")
	}
}

// The workflow runs from the repo root with default flags, so the default
// config and data locations must line up, and data/ must not be ignored.
func TestRepoLayoutForCI(t *testing.T) {
	for _, p := range []string{"../../config/watchlist.yaml", "../../config/sources.yaml", "../../go.mod"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	f, err := os.Open("../../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "data", "data/", "/data", "/data/", "data/*", "/data/*", "*.json", "*.jsonl", "testdata", "/testdata/":
			t.Errorf(".gitignore line %q would hide committed data", sc.Text())
		}
	}
}

// Execute the Run step's script against a stub `go` to check the flags it
// passes for each manual-dispatch input combination.
func TestWatchRunStepArgs(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	script := stepScript(t, readWorkflow(t, "watch.yml"), "Run")

	bin := t.TempDir()
	stub := "#!/bin/sh\necho \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct{ dryRun, only, want string }{
		{"", "", "run ./cmd/fujiwatch run"}, // scheduled run: inputs empty
		{"false", "", "run ./cmd/fujiwatch run"},
		{"true", "", "run ./cmd/fujiwatch run --dry-run"},
		{"false", "vjshop", "run ./cmd/fujiwatch run --only vjshop"},
		{"true", "xspace", "run ./cmd/fujiwatch run --dry-run --only xspace"},
	}
	for _, tt := range tests {
		cmd := exec.CommandContext(t.Context(), bash, "-e", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "DRY_RUN="+tt.dryRun, "ONLY="+tt.only)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tt.want {
			t.Errorf("DRY_RUN=%q ONLY=%q: go %s, want go %s", tt.dryRun, tt.only, got, tt.want)
		}
	}
}

// Execute the Commit data step in a scratch clone with a bare remote.
func TestWatchCommitStep(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	script := stepScript(t, readWorkflow(t, "watch.yml"), "Commit data")

	root := t.TempDir()
	remote, work := filepath.Join(root, "remote.git"), filepath.Join(root, "work")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runStep := func() string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bash, "-e", "-c", script)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("commit step failed: %v\n%s", err, out)
		}
		return string(out)
	}

	git(root, "init", "-q", "--bare", "-b", "main", remote)
	git(root, "clone", "-q", remote, work)
	git(work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	git(work, "push", "-q", "origin", "HEAD:main")

	// No data changes: nothing committed.
	if out := runStep(); !strings.Contains(out, "No data changes.") {
		t.Errorf("output = %q", out)
	}

	// Data changed: committed with the bot identity and pushed.
	if err := os.MkdirAll(filepath.Join(work, "data", "history"), 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(work, "data", "state.json"), []byte("{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(work, "data", "history", "x-t5.jsonl"), []byte("{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(work, "stray.txt"), []byte("not data\n"), 0o600)
	runStep()

	msg := git(remote, "log", "-1", "--format=%s|%an")
	if !regexp.MustCompile(`^data: \d{4}-\d{2}-\d{2} \[skip ci\]\|github-actions\[bot\]$`).MatchString(msg) {
		t.Errorf("pushed commit = %q", msg)
	}
	files := git(remote, "show", "--name-only", "--format=", "HEAD")
	if files != "data/history/x-t5.jsonl\ndata/state.json" {
		t.Errorf("committed files = %q, want only data/", files)
	}
}
