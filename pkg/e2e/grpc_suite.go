package e2e

import (
	"fmt"
	"strings"
	"time"
)

// GRPCMatrix defines the dimensions for gRPC test generation
type GRPCMatrix struct {
	OS        []string
	Languages map[Language][]string // language -> versions
	TLS       []bool
}

// GRPCTestCase represents a single gRPC test configuration
type GRPCTestCase struct {
	Name          string
	Image         GRPCRequestImage
	OS            string
	Language      Language
	Version       string
	Request       *GRPCRequest
	Validations   []ValidationFunc
	ConfigMutator ConfigMutator
}

// GRPCTestSuite contains all generated gRPC test cases
type GRPCTestSuite struct {
	name      string
	testCases []GRPCTestCase
	skipped   []skippedTestCase
}

// GRPCTestSuiteBuilder builds gRPC test suites using the builder pattern
type GRPCTestSuiteBuilder struct {
	name           string
	matrix         GRPCMatrix
	requestBuilder *GRPCRequestBuilder
	validations    []ValidationFunc
	configMutator  ConfigMutator
	errors         []error
}

// NewGRPCTestSuite creates a new gRPC test suite builder
func NewGRPCTestSuite(name string) *GRPCTestSuiteBuilder {
	return &GRPCTestSuiteBuilder{
		name:           name,
		requestBuilder: BuildGRPCRequest(),
		matrix: GRPCMatrix{
			Languages: make(map[Language][]string),
		},
		validations: []ValidationFunc{},
		errors:      []error{},
	}
}

// WithOS adds OS variants to the matrix
func (b *GRPCTestSuiteBuilder) WithOS(os ...string) *GRPCTestSuiteBuilder {
	b.matrix.OS = append(b.matrix.OS, os...)
	return b
}

// WithConfig sets the Qtap config mutator
func (b *GRPCTestSuiteBuilder) WithConfig(fn ConfigMutator) *GRPCTestSuiteBuilder {
	b.configMutator = fn
	return b
}

// WithLanguage adds a language and its versions to the matrix
func (b *GRPCTestSuiteBuilder) WithLanguage(language Language, versions ...string) *GRPCTestSuiteBuilder {
	b.matrix.Languages[language] = append(b.matrix.Languages[language], versions...)
	return b
}

// WithService sets the gRPC service name
func (b *GRPCTestSuiteBuilder) WithService(service string) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithService(service)
	return b
}

// WithMethod sets the gRPC method name
func (b *GRPCTestSuiteBuilder) WithMethod(method string) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithMethod(method)
	return b
}

// WithMessage sets the gRPC request message
func (b *GRPCTestSuiteBuilder) WithMessage(message string) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithMessage(message)
	return b
}

// WithMetadata sets the gRPC metadata
func (b *GRPCTestSuiteBuilder) WithMetadata(metadata map[string]string) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithMetadata(metadata)
	return b
}

// WithTLS adds TLS configurations to the matrix
func (b *GRPCTestSuiteBuilder) WithTLS(tls ...bool) *GRPCTestSuiteBuilder {
	b.matrix.TLS = append(b.matrix.TLS, tls...)
	return b
}

// WithTLSOnly sets the matrix to only test with TLS
func (b *GRPCTestSuiteBuilder) WithTLSOnly() *GRPCTestSuiteBuilder {
	b.matrix.TLS = []bool{true}
	return b
}

// WithPlaintextOnly sets the matrix to only test without TLS
func (b *GRPCTestSuiteBuilder) WithPlaintextOnly() *GRPCTestSuiteBuilder {
	b.matrix.TLS = []bool{false}
	return b
}

// WithBothTLSAndPlaintext sets the matrix to test both TLS and plaintext
func (b *GRPCTestSuiteBuilder) WithBothTLSAndPlaintext() *GRPCTestSuiteBuilder {
	b.matrix.TLS = []bool{true, false}
	return b
}

// WithTimeout sets the gRPC request timeout
func (b *GRPCTestSuiteBuilder) WithTimeout(timeout time.Duration) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithTimeout(timeout)
	return b
}

// WithReadinessHandshake enables the readiness handshake for TLS probe attachment
func (b *GRPCTestSuiteBuilder) WithReadinessHandshake(file string, timeout time.Duration) *GRPCTestSuiteBuilder {
	b.requestBuilder.WithReadinessHandshake(file, timeout)
	return b
}

// WithValidation adds validation functions
func (b *GRPCTestSuiteBuilder) WithValidation(validations ...ValidationFunc) *GRPCTestSuiteBuilder {
	b.validations = append(b.validations, validations...)
	return b
}

// Build generates all gRPC test cases from the matrix
func (b *GRPCTestSuiteBuilder) Build() (*GRPCTestSuite, error) {
	var testCases []GRPCTestCase
	var skipped []skippedTestCase

	for _, os := range b.matrix.OS {
		for lang, versions := range b.matrix.Languages {
			for _, version := range versions {
				cap, exists := GRPCRegistry.Lookup(lang, version, os)
				if !exists {
					skipped = append(skipped,
						skippedTestCase{
							name:   fmt.Sprintf("%s-%s-%s", lang, version, os),
							reason: "no gRPC image found",
						})
					continue
				}

				for _, useTLS := range b.matrix.TLS {
					rb := b.requestBuilder
					rb.WithImageURL(cap.Image.String())
					rb.WithTLS(useTLS)
					if useTLS {
						rb.WithInsecureSkipVerify(true)
					}
					req, err := rb.Build()
					if err != nil {
						return nil, fmt.Errorf("building gRPC request: %w", err)
					}

					tlsStr := ""
					if useTLS {
						tlsStr = "_TLS"
					}

					tc := GRPCTestCase{
						Name: fmt.Sprintf("%s:%s_%s%s",
							lang, version, os, tlsStr),
						Image:         cap.Image,
						OS:            os,
						Language:      lang,
						Version:       version,
						Request:       req,
						Validations:   b.validations,
						ConfigMutator: b.configMutator,
					}

					testCases = append(testCases, tc)
				}
			}
		}
	}

	if len(testCases) == 0 {
		return nil, fmt.Errorf("no valid gRPC test cases generated (skipped: %v)", skipped)
	}

	return &GRPCTestSuite{
		name:      b.name,
		testCases: testCases,
		skipped:   skipped,
	}, nil
}

// PrintTestPlan outputs the gRPC test matrix for review
func (ts *GRPCTestSuite) PrintTestPlan() string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("gRPC Test Suite: %s\n", ts.name))
	result.WriteString(fmt.Sprintf("Total Tests: %d\n\n", len(ts.testCases)))

	byOS := make(map[string][]GRPCTestCase)
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
