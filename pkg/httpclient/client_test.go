package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   string
		responseStatus int
		expectedBody   string
		expectedStatus int
		expectedError  bool
	}{
		{
			name:           "successful GET request",
			responseBody:   `Success response`,
			responseStatus: http.StatusOK,
			expectedBody:   `Success response`,
			expectedStatus: http.StatusOK,
			expectedError:  false,
		},
		{
			name:           "GET request with non-OK status",
			responseBody:   `Error response`,
			responseStatus: http.StatusInternalServerError,
			expectedBody:   ``, // Body should be closed and empty due to non-OK status
			expectedStatus: http.StatusInternalServerError,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.responseStatus)
				fmt.Fprint(w, tt.responseBody)
			}))
			defer server.Close()

			client := &Client{
				client: server.Client(),
			}

			body, statusCode, err := client.Get(server.URL)

			if tt.expectedError && err == nil {
				t.Errorf("Client.Get() expected error, got none")
			}

			if !tt.expectedError && err != nil {
				t.Errorf("Client.Get() unexpected error: %v", err)
			}

			if statusCode != tt.expectedStatus {
				t.Errorf("Client.Get() statusCode = %v, want %v", statusCode, tt.expectedStatus)
			}

			if body != nil {
				defer body.Close()
				bodyBytes, _ := io.ReadAll(body)
				bodyString := string(bodyBytes)
				if bodyString != tt.expectedBody {
					t.Errorf("Client.Get() body = %v, want %v", bodyString, tt.expectedBody)
				}
			}
		})
	}
}

func TestClient_GetJSON(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   string
		responseStatus int
		expectedOut    map[string]string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "successful GET request",
			responseBody:   `{"result":"success"}`,
			responseStatus: http.StatusOK,
			expectedOut:    map[string]string{"result": "success"},
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "server error code",
			responseBody:   `{"error":"internal server error"}`,
			responseStatus: http.StatusInternalServerError,
			expectedOut:    map[string]string{"error": "internal server error"},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.responseStatus)
				fmt.Fprint(w, tt.responseBody)
			}))
			defer server.Close()

			client := &Client{
				client: server.Client(),
			}

			var out map[string]string
			statusCode, err := client.GetJSON(server.URL, &out)

			if (err != nil) != (tt.expectedError != "") {
				t.Errorf("Client.Get() error = %v, expectedError %v", err, tt.expectedError)
			}

			if statusCode != tt.expectedStatus {
				t.Errorf("Client.Get() statusCode = %v, want %v", statusCode, tt.expectedStatus)
			}

			assert.Len(t, out, len(tt.expectedOut))
		})
	}
}

func TestClient_PostJSON(t *testing.T) {
	tests := []struct {
		name                string
		responseBody        string
		responseStatus      int
		input               map[string]string
		expectedOut         map[string]string
		expectedStatus      int
		expectedErrorPrefix string
	}{
		{
			name:                "successful POST request",
			responseBody:        `{"result":"success"}`,
			responseStatus:      http.StatusCreated,
			input:               map[string]string{"key": "value"},
			expectedOut:         map[string]string{"result": "success"},
			expectedStatus:      http.StatusCreated,
			expectedErrorPrefix: "",
		},
		{
			name:                "server error",
			responseBody:        `{"error":"internal server error"}`,
			responseStatus:      http.StatusInternalServerError,
			input:               map[string]string{"key": "value"},
			expectedOut:         map[string]string{},
			expectedStatus:      http.StatusInternalServerError,
			expectedErrorPrefix: "500 Internal Server Error error from API, response dump: HTTP/1.1 500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.responseStatus)
				fmt.Fprint(w, tt.responseBody)
			}))
			defer server.Close()

			client := &Client{
				client: server.Client(),
			}

			var out map[string]string
			statusCode, err := client.PostJSON(server.URL, tt.input, &out)

			if err != nil {
				if tt.expectedErrorPrefix == "" {
					t.Errorf("Unexpected error: %s", err.Error())
					return
				}

				if !strings.HasPrefix(err.Error(), tt.expectedErrorPrefix) {
					t.Errorf("Expected error to have prefx %s but was %s", tt.expectedErrorPrefix, err.Error())
					return
				}
			}

			if statusCode != tt.expectedStatus {
				t.Errorf("Client.Post() statusCode = %v, want %v", statusCode, tt.expectedStatus)
			}

			assert.Len(t, out, len(tt.expectedOut))
		})
	}
}
