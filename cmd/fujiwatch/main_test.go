package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMatchCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"match", "--config", "../../config", "Fujifilm XT3 body cũ 98%"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{
		"normalised: fujifilm x-t3 body cu 98%",
		"result:     matched x-t3",
		"condition:  used",
		"✓  x-t3",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != exitUsage {
		t.Errorf("no args: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"bogus"}, &out, &errOut); code != exitUsage {
		t.Errorf("unknown command: exit %d, want %d", code, exitUsage)
	}
	if code := run([]string{"match"}, &out, &errOut); code != exitUsage {
		t.Errorf("match without title: exit %d, want %d", code, exitUsage)
	}
}
