package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurity(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for security script tests", tool)
		}
	}

	const osv = `{"osv":{"id":"GO-2026-5932","aliases":["GHSA-test","CVE-2026-1234"],"summary":"OpenPGP is unmaintained"}}` + "\n"
	const moduleFinding = `{"finding":{"osv":"GO-2026-5932","trace":[{"module":"golang.org/x/crypto","version":"v0.56.0"}]}}` + "\n"
	const packageFinding = `{"finding":{"osv":"GO-2026-5932","trace":[{"module":"golang.org/x/crypto","package":"golang.org/x/crypto/openpgp"}]}}` + "\n"
	const symbolFinding = `{"finding":{"osv":"GO-2026-5932","fixed_version":"v0.57.0","trace":[{"module":"golang.org/x/crypto","package":"golang.org/x/crypto/openpgp","function":"ReadMessage"}]}}` + "\n"
	const otherFinding = `{"finding":{"osv":"GO-2026-9999","trace":[{"module":"example.com/vulnerable","package":"example.com/vulnerable"}]}}` + "\n"

	tests := []struct {
		name         string
		output       string
		allowlist    string
		missingAllow bool
		scannerExit  int
		wantExit     int
		want         []string
		unwanted     []string
	}{
		{
			name:     "clean scan with empty allowlist",
			output:   `{"config":{"scan_level":"symbol"}}`,
			want:     []string{"No active vulnerabilities."},
			unwanted: []string{"Suppressed vulnerabilities"},
		},
		{
			name:      "module-only finding needs no exception",
			output:    osv + moduleFinding,
			allowlist: "# No exceptions\n \t\n",
			want:      []string{"No active vulnerabilities."},
			unwanted:  []string{"GO-2026-5932", "Suppressed vulnerabilities"},
		},
		{
			name:      "module-only finding is not reported as suppressed",
			output:    osv + moduleFinding,
			allowlist: "GO-2026-5932\n",
			want:      []string{"No active vulnerabilities."},
			unwanted:  []string{"GO-2026-5932", "Suppressed vulnerabilities"},
		},
		{
			name:         "importing OpenPGP fails without an allowlist",
			output:       osv + moduleFinding + packageFinding,
			missingAllow: true,
			wantExit:     1,
			want:         []string{"Active vulnerabilities (1):", "GO-2026-5932"},
			unwanted:     []string{"No active vulnerabilities.", "Suppressed vulnerabilities"},
		},
		{
			name:     "reachable finding includes details without counting duplicates",
			output:   osv + moduleFinding + packageFinding + symbolFinding + symbolFinding,
			wantExit: 1,
			want:     []string{"Active vulnerabilities (1):", "reachable symbols: golang.org/x/crypto/openpgp.ReadMessage", "fixed in: v0.57.0"},
		},
		{
			name:      "allowlist matches Go ID",
			output:    osv + moduleFinding + packageFinding,
			allowlist: "GO-2026-5932\n",
			want:      []string{"Suppressed vulnerabilities (1)", "GO-2026-5932", "No active vulnerabilities."},
		},
		{
			name:      "allowlist matches GHSA ignoring case and comments",
			output:    osv + packageFinding,
			allowlist: "# Accepted risk\n \tghsa-test \t# explanation\r\n",
			want:      []string{"Suppressed vulnerabilities (1)", "No active vulnerabilities."},
		},
		{
			name:      "allowlist matches CVE",
			output:    osv + packageFinding,
			allowlist: "CVE-2026-1234\n",
			want:      []string{"Suppressed vulnerabilities (1)", "No active vulnerabilities."},
		},
		{
			name:      "allowlisting one finding does not suppress another",
			output:    osv + packageFinding + otherFinding,
			allowlist: "GO-2026-5932\n",
			wantExit:  1,
			want:      []string{"Suppressed vulnerabilities (1)", "Active vulnerabilities (1):", "GO-2026-9999"},
			unwanted:  []string{"No active vulnerabilities."},
		},
		{
			name:        "scanner failure cannot report success",
			allowlist:   "GO-2026-5932\n",
			scannerExit: 2,
			wantExit:    2,
			want:        []string{"simulated scanner failure"},
			unwanted:    []string{"No active vulnerabilities."},
		},
		{
			name:        "partial scan cannot report success",
			output:      osv + moduleFinding,
			allowlist:   "GO-2026-5932\n",
			scannerExit: 1,
			wantExit:    1,
			want:        []string{"simulated scanner failure"},
			unwanted:    []string{"No active vulnerabilities.", "Suppressed vulnerabilities"},
		},
		{
			name:      "malformed scanner output fails",
			output:    "invalid json",
			allowlist: "GO-2026-5932\n",
			wantExit:  -1,
			unwanted:  []string{"No active vulnerabilities."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			allowlist := filepath.Join(tmp, "allowlist")
			if !tt.missingAllow {
				require.NoError(t, os.WriteFile(allowlist, []byte(tt.allowlist), 0o600))
			}
			fixture := filepath.Join(tmp, "scanner.json")
			require.NoError(t, os.WriteFile(fixture, []byte(tt.output), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(tmp, "go"), []byte(`#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "tool govulncheck -format=json ./..." ]]
cat "$SECURITY_TEST_FIXTURE"
if [[ "$SECURITY_TEST_EXIT" != 0 ]]; then
  echo "simulated scanner failure" >&2
fi
exit "$SECURITY_TEST_EXIT"
`), 0o700))
			scratch := filepath.Join(tmp, "scratch")
			require.NoError(t, os.Mkdir(scratch, 0o700))
			t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("ALLOWLIST", allowlist)
			t.Setenv("TMPDIR", scratch)
			t.Setenv("SECURITY_TEST_FIXTURE", fixture)
			t.Setenv("SECURITY_TEST_EXIT", strconv.Itoa(tt.scannerExit))

			cmd := exec.CommandContext(t.Context(), "bash", "security.sh")
			output, err := cmd.CombinedOutput()
			if tt.wantExit == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				if tt.wantExit > 0 {
					assert.Equal(t, tt.wantExit, exitErr.ExitCode(), "%s", output)
				}
			}
			for _, want := range tt.want {
				assert.Contains(t, string(output), want)
			}
			for _, unwanted := range tt.unwanted {
				assert.NotContains(t, string(output), unwanted)
			}
			files, err := os.ReadDir(scratch)
			require.NoError(t, err)
			assert.Empty(t, files, "security script must clean up temporary files")
		})
	}
}
