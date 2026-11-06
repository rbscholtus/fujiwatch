// Command fujiwatch watches Fujifilm X-mount camera and lens prices in
// Vietnamese camera shops and alerts on price drops and new listings.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  fujiwatch run [--dry-run] [--only <shop>] [--no-notify]
  fujiwatch snapshot <url>
  fujiwatch extract <shop> <file.html>
  fujiwatch match "<title>"`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	switch cmd := os.Args[1]; cmd {
	case "run", "snapshot", "extract", "match":
		fmt.Fprintf(os.Stderr, "fujiwatch: %q is not implemented yet\n", cmd)
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "fujiwatch: unknown command %q\n\n%s\n", cmd, usage)
		os.Exit(2)
	}
}
