//go:build linux

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process"
)

func expectLines(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// These checks need no BPF privileges: they populate a process value directly
// and verify the rendered text.
func TestDescribeQuotesFields(t *testing.T) {
	p := process.NewProcess(42, "", nil)
	p.Exe = "/opt/my app/bin/server"
	p.Binary = "server"
	p.Args = []string{"server", "--name", "two words", "line1\nline2"}
	p.Cgroup = "/kubepods/pod1234"
	p.ContainerID = "abcdef123456"
	p.PodID = "1234"
	p.Root = "/proc/42/root"
	p.SetUser(1000, "svc user")

	d := capture(p)
	d.tracked = true
	out := describe("started", d)

	expectLines(t, out,
		"started pid=42 existing=false tracked=true\n",
		`exe:       "/opt/my app/bin/server"`,
		`binary:    "server"`,
		`"two words"`,
		`"line1\nline2"`,
		`user:      "svc user" uid=1000`,
		`cgroup:    "/kubepods/pod1234"`,
		`container: "abcdef123456"`,
		`pod:       "1234"`,
		`root:      "/proc/42/root"`,
	)
	if got := strings.Count(out, "\n"); got != 9 {
		t.Errorf("expected 9 lines, got %d:\n%s", got, out)
	}
}

func TestDescribeMissingArgumentsAndIdentity(t *testing.T) {
	// The test's own PID resolves through the real lazy lookup.
	p := process.NewProcess(os.Getpid(), "", nil)
	p.Exe, p.Binary, p.PredatesQpoint = "/bin/sleep", "sleep", true
	p.ContainerID = "root"

	out := describe("replaced", capture(p))
	if !strings.HasPrefix(out, "replaced pid=") || strings.Contains(out, "tracked=") {
		t.Errorf("replaced header should not include a registry lookup:\n%s", out)
	}
	expectLines(t, out,
		"existing=true\n",
		"args:      not captured",
		"user:      \"",
		`container: "root"`,
		"cgroup:    unavailable",
		"pod:       unavailable",
		"root:      unavailable",
	)
}

func TestDescribeUserUnavailable(t *testing.T) {
	// A PID that cannot exist makes the lookup fail with no user at all.
	p := process.NewProcess(1<<30, "", nil)
	out := describe("started", capture(p))
	expectLines(t, out, "user:      unavailable (")
	if strings.Contains(out, "uid=") {
		t.Errorf("no uid should be shown for a failed lookup:\n%s", out)
	}

	// A resolved UID whose name lookup failed keeps the UID visible, even zero.
	p.SetUser(0, "")
	out = describe("started", capture(p))
	expectLines(t, out, "user:      unavailable uid=0")
}
