package gobin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSupportedGoVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Unsupported versions
		{input: "1.12", want: false},
		{input: "1.12.1", want: false},
		{input: "1.12.12", want: false},
		{input: "1.13beta1", want: false},
		{input: "1.13rc1", want: false},
		{input: "1.13", want: false},
		{input: "1.13.1", want: false},
		{input: "1.13.15", want: false},

		// Supported versions
		{input: "1.17", want: true},
		{input: "1.17beta1", want: true},
		{input: "1.17rc1", want: true},
		{input: "1.17rc2", want: true},
		{input: "1.17.1", want: true},
		{input: "1.17.13", want: true},
		{input: "1.18", want: true},
		{input: "1.18.9", want: true},
		{input: "1.21.13 X:boringcrypto", want: true},
		{input: "1.23-20240712-RC01 cl/651910239 +071b8d51c1 X:fieldtrack,boringcrypto", want: true},
		{input: "1.24-20241216-RC00 cl/706826196 +d92c34a387 X:fieldtrack,boringcrypto", want: true},

		// Uncleaned Go version strings
		{input: "go1.13.4", want: false},
		{input: "go1.21.4", want: true},
		{input: "devel go1.22-098f059 Mon Dec 4 23:03:04 2023 +0000", want: true},

		// Invalid versions
		{input: "devel", want: false},
		{input: "go", want: false},
		{input: "098f059", want: false},
		{input: "Mon Dec 4 23:03:04 2023 +0000", want: false},

		{input: "1.21.13+Xboringcrypto", want: true},
		{input: "1.23-20240712-RC01 cl/651910239 +071b8d51c1 X:fieldtrack,boringcrypto", want: true},
		{input: "1.24-20241216-RC00 cl/706826196 +d92c34a387 X:fieldtrack,boringcrypto", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SupportedGoVersion(tt.input)
			assert.Equal(t, tt.want, got, "input: %v", tt.input)
		})
	}
}
