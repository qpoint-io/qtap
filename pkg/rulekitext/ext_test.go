package rulekitext

import (
	"fmt"
	"testing"

	"github.com/qpoint-io/rulekit"
	"github.com/stretchr/testify/require"
)

func TestZone(t *testing.T) {
	tcs := map[string]string{
		"example.com":           "example.com",
		"sub.example.com":       "example.com",
		"test.some-unknown-tld": "test.some-unknown-tld",
		"localhost":             "localhost",
		"some.sub.test.s3-website.us-west-1.amazonaws.com": "test.s3-website.us-west-1.amazonaws.com",
	}

	for domain, want := range tcs {
		t.Run(domain, func(t *testing.T) {
			r, err := rulekit.Parse("zone(domain)")
			require.NoError(t, err)

			res := r.Eval(&rulekit.Ctx{
				Functions: Functions,
				KV: rulekit.KV{
					"domain": domain,
				},
			})
			require.True(t, res.Ok())
			require.Equal(t, want, res.Value)

			require.Equal(t, want, Zone(domain))
		})
	}
}

func TestInZone(t *testing.T) {
	tcs := []struct {
		domain string
		zone   string
		want   bool
	}{
		{
			domain: "example.com",
			zone:   "example.com",
			want:   true,
		},
		{
			domain: "sub.example.com",
			zone:   "example.com",
			want:   true,
		},
		{
			domain: "api.example.com",
			zone:   "internal.example.com",
			want:   false,
		},
		{
			domain: "test.com",
			zone:   "example.com",
			want:   false,
		},
	}

	for _, tc := range tcs {
		t.Run(fmt.Sprintf("%s,%s", tc.domain, tc.zone), func(t *testing.T) {
			require.Equal(t, tc.want, InZone(tc.domain, tc.zone))
		})
	}
}
