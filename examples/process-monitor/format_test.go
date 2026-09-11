//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process"
)

// These checks need no BPF privileges: they populate a process value directly
// and verify the rendered text.
func TestDescribeQuotesFields(t *testing.T) {
	p := process.NewProcess(42, "", nil)
	p.Exe = "/opt/my app/bin/server"
	p.Binary = "server"
	p.Args = []string{"server", "--name", "two words", "line1\nline2"}

	d := capture(p)
	d.tracked = true
	out := describe("started", d)

	for _, want := range []string{
		"started pid=42 existing=false tracked=true\n",
		`exe:    "/opt/my app/bin/server"`,
		`binary: "server"`,
		`"two words"`,
		`"line1\nline2"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "\n"); got != 4 {
		t.Errorf("expected 4 lines, got %d:\n%s", got, out)
	}
}

func TestDescribeMissingArguments(t *testing.T) {
	p := process.NewProcess(7, "", nil)
	p.Exe, p.Binary, p.PredatesQpoint = "/bin/sleep", "sleep", true

	out := describe("replaced", capture(p))
	if !strings.HasPrefix(out, "replaced pid=7 existing=true\n") {
		t.Errorf("unexpected header:\n%s", out)
	}
	if strings.Contains(out, "tracked=") {
		t.Errorf("replaced events should not query the registry:\n%s", out)
	}
	if !strings.Contains(out, "args:   not captured") {
		t.Errorf("empty arguments must be reported as not captured:\n%s", out)
	}
}
