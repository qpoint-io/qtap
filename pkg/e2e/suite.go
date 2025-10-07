package e2e

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Matrix defines the dimensions for test generation
type Matrix struct {
	OS           []string
	Languages    map[Language][]string // language -> versions
	HTTPProtocol []HTTPProtocol
	TLS          []bool                // whether to use TLS
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
	skipped   []skippedTestCase
	handler   http.Handler
}

type skippedTestCase struct {
	name   string
	reason string
}

// TestSuiteBuilder builds test suites using the builder pattern
type TestSuiteBuilder struct {
	name string
	// registry           *ImageRegistry
	matrix         Matrix
	requestBuilder *HTTPRequestBuilder
	validations    []ValidationFunc
	configMutator  ConfigMutator
	handler        http.Handler
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
func (b *TestSuiteBuilder) WithMethod(method HTTPMethod) *TestSuiteBuilder {
	b.requestBuilder.WithMethod(method)
	return b
}

func (b *TestSuiteBuilder) WithURL(url string) *TestSuiteBuilder {
	b.requestBuilder.WithURL(url)
	return b
}

func (b *TestSuiteBuilder) WithHTTPProtocols(protos ...HTTPProtocol) *TestSuiteBuilder {
	b.matrix.HTTPProtocol = append(b.matrix.HTTPProtocol, protos...)
	return b
}

func (b *TestSuiteBuilder) WithTLS(tls ...bool) *TestSuiteBuilder {
	b.matrix.TLS = append(b.matrix.TLS, tls...)
	return b
}

func (b *TestSuiteBuilder) WithTLSOnly() *TestSuiteBuilder {
	b.matrix.TLS = []bool{true}
	return b
}

func (b *TestSuiteBuilder) WithPlaintextOnly() *TestSuiteBuilder {
	b.matrix.TLS = []bool{false}
	return b
}

func (b *TestSuiteBuilder) WithBothTLSAndPlaintext() *TestSuiteBuilder {
	b.matrix.TLS = []bool{true, false}
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

// Handler configuration
func (b *TestSuiteBuilder) WithHandler(handler http.Handler) *TestSuiteBuilder {
	b.handler = handler
	return b
}

// Build generates all test cases
func (b *TestSuiteBuilder) Build() (*TestSuite, error) {
	var testCases []TestCase
	var skipped []skippedTestCase

	// Generate test matrix
	for _, os := range b.matrix.OS {
		for lang, versions := range b.matrix.Languages {
			for _, version := range versions {
				// Look up image capabilities
				cap, exists := Registry.Lookup(lang, version, os)
				if !exists {
					skipped = append(skipped,
						skippedTestCase{
							name:   fmt.Sprintf("%s-%s-%s", lang, version, os),
							reason: "no image found",
						})
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
							skippedTestCase{
								name: fmt.Sprintf("%s-%s-%s-%s",
									lang, version, os, clientName),
								reason: "missing client",
							})
						continue
					}

					// Test each HTTP Protocol
					for _, httpProto := range b.matrix.HTTPProtocol {
						// Check if client supports this HTTP protocol
						// Skip at matrix generation time - no runtime fallbacks
						if !slices.Contains(clientCap.HTTPProtocols, httpProto) {
							skipped = append(skipped,
								skippedTestCase{
									name: fmt.Sprintf("%s-%s-%s-%s-%s",
										lang, version, os, clientName, httpProto),
									reason: "doesn't support " + httpProto.String(),
								})
							continue
						}

						// Test each TLS configuration
						for _, useTLS := range b.matrix.TLS {
							// HTTP/2 requires TLS, HTTP/1.0 doesn't support TLS
							if httpProto == HTTPProtocolHTTP2_0 && !useTLS {
								skipped = append(skipped,
									skippedTestCase{
										name: fmt.Sprintf("%s-%s-%s/%s/%s",
											lang, version, os, clientName, httpProto),
										reason: "HTTP/2 requires TLS",
									})
								continue
							}
							if httpProto == HTTPProtocolHTTP1_0 && useTLS {
								skipped = append(skipped,
									skippedTestCase{
										name: fmt.Sprintf("%s-%s-%s/%s/%s",
											lang, version, os, clientName, httpProto),
										reason: "HTTP/1.0 with TLS not supported",
									})
								continue
							}

							// Create test case
							rb := b.requestBuilder
							rb.WithImageURL(cap.Image.String())
							rb.WithClient(clientName)
							rb.WithProtocol(httpProto)
							rb.WithTLS(useTLS)
							req, err := rb.Build()
							if err != nil {
								return nil, fmt.Errorf("building request: %w", err)
							}

							tlsStr := ""
							if useTLS {
								tlsStr = "_TLS"
							}

							tc := TestCase{
								Name: fmt.Sprintf("%s:%s_%s_%s_%s%s_%s",
									lang, version, os, clientName, httpProto, tlsStr, req.Method),
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
	}

	// Check if any test cases were generated
	if len(testCases) == 0 {
		return nil, fmt.Errorf("no valid test cases generated (skipped: %v)", skipped)
	}

	return &TestSuite{
		name:      b.name,
		testCases: testCases,
		skipped:   skipped,
		handler:   b.handler,
	}, nil
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
