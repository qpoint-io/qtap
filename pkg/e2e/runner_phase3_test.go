package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPServerGroupsSeparateTLSAndPlaintext(t *testing.T) {
	runner := TestSuiteRunner{Suite: &TestSuite{testCases: []TestCase{
		{Request: &HTTPRequest{Proto: HTTPProtocolHTTP1_1, TLS: true}},
		{Request: &HTTPRequest{Proto: HTTPProtocolHTTP1_1, TLS: false}},
	}}}

	require.Len(t, runner.groupTestsByHTTPProto(), 2)
}

func TestHTTPServerGroupsIgnoreTestsWithoutRequests(t *testing.T) {
	runner := TestSuiteRunner{Suite: &TestSuite{testCases: []TestCase{{Request: nil}}}}

	require.NotPanics(t, func() {
		require.Empty(t, runner.groupTestsByHTTPProto())
	})
}
