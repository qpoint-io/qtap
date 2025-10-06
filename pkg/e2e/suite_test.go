package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestSuiteBuilder(t *testing.T) {
	// Initialize registry with some Python images for testing
	setupTestRegistry()

	// Build a test suite as shown in the user's example
	suite, err := NewTestSuite("Basic HTTP GET").
		WithOS("alpine", "debian").
		WithLanguage(Python, "3.10.0", "3.12.0").
		WithMethod(HTTPMethodGet).
		WithURL("/api/health").
		WithHTTPProtocols(HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0).
		WithBothTLSAndPlaintext().
		Build()

	require.NoError(t, err)
	require.NotNil(t, suite)

	// // Print the test plan for review
	// t.Log(suite.PrintTestPlan())

	// Verify the suite has the expected structure
	require.Equal(t, "Basic HTTP GET", suite.name)
	require.NotEmpty(t, suite.testCases, "should have generated test cases")

	// Check that test cases were generated for the matrix
	t.Logf("Generated %d test cases", len(suite.testCases))
	for i, tc := range suite.testCases {
		t.Logf("Test case %d: %s", i+1, tc.Name)
		require.NotEmpty(t, tc.Name)
		require.Equal(t, Python, tc.Language)
		require.Contains(t, []string{"alpine", "debian"}, tc.OS)
		require.Contains(t, []string{"3.10.0", "3.12.0"}, tc.Version)
		require.NotNil(t, tc.Request)
		require.Equal(t, HTTPMethodGet, tc.Request.Method)
		require.Equal(t, "/api/health", tc.Request.URL)
		require.Contains(t, []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1, HTTPProtocolHTTP2_0}, tc.Request.Proto)
	}
}

// setupTestRegistry initializes the registry with test image capabilities
func setupTestRegistry() {
	// Clear any existing registrations for clean tests
	Registry = NewImageRegistry()

	// Register Python images with client capabilities
	clients := map[string]*ClientCapabilities{
		"requests": {
			Name:          "requests",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1},
		},
		"urllib3": {
			Name:          "urllib3",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP1_0, HTTPProtocolHTTP1_1},
		},
		"httpx": {
			Name:          "httpx",
			HTTPProtocols: []HTTPProtocol{HTTPProtocolHTTP2_0},
		},
	}

	// Register Python 3.10.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_10_0_Alpine,
		Language: Python,
		Version:  "3.10.0",
		OS:       "alpine",
		Clients:  clients,
	})

	// Register Python 3.10.0 Debian
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_10_0_Bullseye,
		Language: Python,
		Version:  "3.10.0",
		OS:       "debian",
		Clients:  clients,
	})

	// Register Python 3.12.0 Alpine
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_12_0_Alpine,
		Language: Python,
		Version:  "3.12.0",
		OS:       "alpine",
		Clients:  clients,
	})

	// Register Python 3.12.0 Debian
	Registry.Register(&ImageCapabilities{
		Image:    HTTPRequestPython3_12_0_Bullseye,
		Language: Python,
		Version:  "3.12.0",
		OS:       "debian",
		Clients:  clients,
	})
}
