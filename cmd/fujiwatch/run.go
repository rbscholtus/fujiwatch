package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/notify"
	"github.com/rbscholtus/fujiwatch/internal/runner"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// runFlags are the options of `fujiwatch run`.
type runFlags struct {
	cfgDir   string
	dataDir  string
	only     string
	dryRun   bool
	noNotify bool
}

// fetcherFactory builds the HTTP client; tests replace it.
var fetcherFactory = func(o fetch.Options) runner.Fetcher { return fetch.New(o) }

// cmdRun runs one watch cycle.
func cmdRun(args []string, stdout, stderr io.Writer) int {
	var rf runFlags
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&rf.cfgDir, "config", "config", "directory with watchlist.yaml and sources.yaml")
	fs.StringVar(&rf.dataDir, "data", "data", "directory with state.json and history/")
	fs.StringVar(&rf.only, "only", "", "only run the sources of this shop")
	fs.BoolVar(&rf.dryRun, "dry-run", false, "fetch and diff, but don't write state/history or send notifications")
	fs.BoolVar(&rf.noNotify, "no-notify", false, "don't send notifications")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}

	if err := loadDotEnv(".env"); err != nil {
		return errorf(stderr, "%v", err)
	}
	cfg, err := loadConfig(rf.cfgDir)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	if rf.only != "" && !hasShop(cfg.Sources.Sources, rf.only) {
		return errorf(stderr, "no sources for shop %q", rf.only)
	}

	statePath := filepath.Join(rf.dataDir, "state.json")
	st, err := state.Load(statePath)
	if err != nil {
		return errorf(stderr, "%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	f := fetcherFactory(fetch.Options{
		Timeout: cfg.Sources.Defaults.Timeout(),
		Delay:   cfg.Sources.Defaults.Delay(),
	})
	rep, hist, err := runner.Run(ctx, cfg, st, f, runner.Options{Only: rf.only, Log: stderr})
	if err != nil {
		return errorf(stderr, "%v", err)
	}

	if err := runner.WriteText(stdout, rep, rf.dryRun); err != nil {
		return errorf(stderr, "%v", err)
	}
	if rf.dryRun {
		return exitOK
	}

	if err := state.AppendHistory(filepath.Join(rf.dataDir, "history"), hist); err != nil {
		return errorf(stderr, "%v", err)
	}
	if err := st.Save(statePath); err != nil {
		return errorf(stderr, "%v", err)
	}

	if !rf.noNotify {
		sendNotifications(ctx, cfg, rep, st, filepath.Join(rf.dataDir, "history"), stderr)
	}
	return exitOK
}

// notifierFactory builds notifiers from the environment; tests replace it.
var notifierFactory = func() (email, whatsapp notify.Notifier, problems []string) {
	e, w, problems := notify.FromEnv(os.Getenv)
	if e != nil {
		email = e
	}
	if w != nil {
		whatsapp = w
	}
	return email, whatsapp, problems
}

// sendNotifications delivers the run's results. Failures are warnings, not
// errors: the run's state must still be committed by CI.
func sendNotifications(ctx context.Context, cfg *config.Config, rep *runner.Report, st *state.State, historyDir string, stderr io.Writer) {
	email, whatsapp, problems := notifierFactory()
	for _, p := range problems {
		warn(stderr, p)
	}
	msg, err := notify.BuildMessage(cfg, rep, st, historyDir)
	if err != nil {
		warn(stderr, err.Error())
		return
	}

	if whatsapp != nil {
		if err := whatsapp.Send(ctx, msg); err != nil {
			warn(stderr, err.Error())
		}
	}
	switch {
	case email == nil:
	case !notify.ShouldEmail(cfg.Sources.Notify, rep.Events, rep.Time):
		_, _ = fmt.Fprintln(stderr, "email: no changes, not sending (notify.email: changes)")
	default:
		if err := email.Send(ctx, msg); err != nil {
			warn(stderr, err.Error())
		}
	}
}

// warn prints a warning, as a GitHub Actions annotation when running there.
func warn(w io.Writer, msg string) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		_, _ = fmt.Fprintf(w, "::warning::%s\n", msg)
		return
	}
	_, _ = fmt.Fprintf(w, "fujiwatch: warning: %s\n", msg)
}
