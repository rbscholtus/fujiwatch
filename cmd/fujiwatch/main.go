// Command fujiwatch watches Fujifilm X-mount camera and lens prices in
// Vietnamese camera shops and alerts on price drops and new listings.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rbscholtus/fujiwatch/internal/config"
)

const usage = `usage:
  fujiwatch run [--dry-run] [--only <shop>] [--no-notify]
  fujiwatch snapshot [--config <dir>] [--out <dir>] [--shop <shop>] [--mode product|listing] [--no-rewrite] <url>
  fujiwatch extract [--config <dir>] [--url <page-url>] <shop> <file>
  fujiwatch match [--config <dir>] "<title>" [url]`

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}

	switch cmd := args[0]; cmd {
	case "match":
		return cmdMatch(args[1:], stdout, stderr)
	case "snapshot":
		return cmdSnapshot(args[1:], stdout, stderr)
	case "extract":
		return cmdExtract(args[1:], stdout, stderr)
	case "run":
		_, _ = fmt.Fprintf(stderr, "fujiwatch: %q is not implemented yet\n", cmd)
		return exitError
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, usage)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "fujiwatch: unknown command %q\n\n%s\n", cmd, usage)
		return exitUsage
	}
}

func loadConfig(dir string) (*config.Config, error) {
	cfg, err := config.Load(filepath.Join(dir, "watchlist.yaml"), filepath.Join(dir, "sources.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// errorf prints a "fujiwatch: ..." error and returns exitError.
func errorf(stderr io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(stderr, "fujiwatch: "+format+"\n", args...)
	return exitError
}
