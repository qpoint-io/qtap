package e2e

import (
	"fmt"
	"strings"
	"time"
)

// Matrix defines the dimensions for test generation
type Matrix struct {
	OS           []string
	Languages    map[Language][]string // language -> versions
	HTTPVersions []string
	Clients      map[Language][]string // language -> clients
}

// TestCase represents a single test configuration
type TestCase struct {
	Name          string
	Image         HTTPRequestImage
	OS            string
	Language      Language
	Version       string
	Client        string
	Request       *HTTPRequest
	Validations   []ValidationFunc
	ConfigMutator ConfigMutator
}

// TestSuite contains all generated test cases
type TestSuite struct {
	name      string
	testCases []TestCase
	skipped   []string
}

// TestSuiteBuilder builds test suites using the builder pattern
type TestSuiteBuilder struct {
	name string
	// registry           *ImageRegistry
	matrix         Matrix
	requestBuilder *HTTPRequestBuilder
	validations    []ValidationFunc
	configMutator  ConfigMutator
	errors         []error
}

// NewTestSuite creates a new test suite builder
func NewTestSuite(name string) *TestSuiteBuilder {
	return &TestSuiteBuilder{
		name: name,
		// registry:       Registry,
		requestBuilder: BuildHTTPRequest(),
		matrix: Matrix{
			Languages: make(map[Language][]string),
			Clients:   make(map[Language][]string),
		},
		validations: []ValidationFunc{},
		errors:      []error{},
	}
}

// OS configuration
func (b *TestSuiteBuilder) WithOS(os ...string) *TestSuiteBuilder {
	b.matrix.OS = append(b.matrix.OS, os...)
	return b
}

// Qtap configuration
func (b *TestSuiteBuilder) WithConfig(fn ConfigMutator) *TestSuiteBuilder {
	b.configMutator = fn
	return b
}

// Language version configuration
func (b *TestSuiteBuilder) WithLanguage(language Language, versions ...string) *TestSuiteBuilder {
	b.matrix.Languages[language] = append(b.matrix.Languages[language], versions...)
	return b
}

// HTTP configuration (proxies to request builder)
func (b *TestSuiteBuilder) WithMethod(method string) *TestSuiteBuilder {
	b.requestBuilder.WithMethod(method)
	return b
}

func (b *TestSuiteBuilder) WithURL(url string) *TestSuiteBuilder {
	b.requestBuilder.WithURL(url)
	return b
}

func (b *TestSuiteBuilder) WithHTTPVersions(versions ...string) *TestSuiteBuilder {
	b.matrix.HTTPVersions = append(b.matrix.HTTPVersions, versions...)
	return b
}

func (b *TestSuiteBuilder) WithHeader(key, value string) *TestSuiteBuilder {
	b.requestBuilder.WithHeader(key, value)
	return b
}

func (b *TestSuiteBuilder) WithBody(body string) *TestSuiteBuilder {
	b.requestBuilder.WithBody(body)
	return b
}

// Client configuration
func (b *TestSuiteBuilder) WithLanguageClients(language Language, clients ...string) *TestSuiteBuilder {
	b.matrix.Clients[language] = append(b.matrix.Clients[language], clients...)
	return b
}

// Validation configuration
func (b *TestSuiteBuilder) WithValidation(validations ...ValidationFunc) *TestSuiteBuilder {
	b.validations = append(b.validations, validations...)
	return b
}

// Startup delay configuration
func (b *TestSuiteBuilder) WithStartupDelay(delay time.Duration) *TestSuiteBuilder {
	b.requestBuilder.WithStartupDelay(delay)
	return b
}

// Build generates all test cases
func (b *TestSuiteBuilder) Build() (*TestSuite, error) {
	var testCases []TestCase
	var skipped []string

	// Generate test matrix
	for _, os := range b.matrix.OS {
		for lang, versions := range b.matrix.Languages {
			for _, version := range versions {
				// Look up image capabilities
				cap, exists := Registry.Lookup(lang, version, os)
				if !exists {
					skipped = append(skipped,
						fmt.Sprintf("%s-%s-%s: no image found", lang, version, os))
					continue
				}

				// Determine which clients to test
				requestedClients := b.matrix.Clients[lang]
				if len(requestedClients) == 0 {
					// Use all available clients if none specified
					for clientName := range cap.Clients {
						requestedClients = append(requestedClients, clientName)
					}
				}

				// Generate test cases for each client
				for _, clientName := range requestedClients {
					clientCap, hasClient := cap.Clients[clientName]
					if !hasClient {
						skipped = append(skipped,
							fmt.Sprintf("%s-%s-%s: missing client %s",
								lang, version, os, clientName))
						continue
					}

					// Test each HTTP version
					for _, httpVersion := range b.matrix.HTTPVersions {
						// Check if client supports this HTTP version
						// Skip at matrix generation time - no runtime fallbacks
						if !contains(clientCap.HTTPVersions, httpVersion) {
							skipped = append(skipped,
								fmt.Sprintf("%s-%s-%s/%s: doesn't support HTTP/%s",
									lang, version, os, clientName, httpVersion))
							continue
						}

						// Create test case
						rb := b.requestBuilder
						rb.WithImageURL(string(cap.Image))
						rb.WithClient(clientName)
						rb.WithHTTPVersion(httpVersion)
						req, err := rb.Build()
						if err != nil {
							return nil, fmt.Errorf("building request: %w", err)
						}

						tc := TestCase{
							Name: fmt.Sprintf("%s/%s-%s/%s/HTTP%s/%s",
								os, lang, version, clientName, httpVersion, req.Method),
							Image:         cap.Image,
							OS:            os,
							Language:      lang,
							Version:       version,
							Client:        clientName,
							Request:       req,
							Validations:   b.validations,
							ConfigMutator: b.configMutator,
						}

						testCases = append(testCases, tc)
					}
				}
			}
		}
	}

	// Check if any test cases were generated
	if len(testCases) == 0 {
		return nil, fmt.Errorf("no valid test cases generated (skipped: %v)", skipped)
	}

	return &TestSuite{
		name:      b.name,
		testCases: testCases,
		skipped:   skipped,
	}, nil
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// PrintTestPlan outputs the test matrix for review
func (ts *TestSuite) PrintTestPlan() string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("Test Suite: %s\n", ts.name))
	result.WriteString(fmt.Sprintf("Total Tests: %d\n\n", len(ts.testCases)))

	// Group by OS for readability
	byOS := make(map[string][]TestCase)
	for _, tc := range ts.testCases {
		byOS[tc.OS] = append(byOS[tc.OS], tc)
	}

	for os, cases := range byOS {
		result.WriteString(os + ":\n")
		for _, tc := range cases {
			result.WriteString(fmt.Sprintf("  • %s\n", tc.Name))
		}
	}

	if len(ts.skipped) > 0 {
		result.WriteString("\nSkipped Combinations:\n")
		for _, skip := range ts.skipped {
			result.WriteString(fmt.Sprintf("  • %s\n", skip))
		}
	}

	return result.String()
}
