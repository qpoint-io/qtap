package register

import (
	"errors"
	"net/http"
	"testing"

	"github.com/qpoint-io/qtap/pkg/httpclient/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestFetchRegistration(t *testing.T) {
	// Define the test cases
	tests := []struct {
		name           string
		endpoint       string
		mockStatusCode int
		mockResponse   RegistrationResponse
		mockError      error
		expectedResult *Registration
		expectError    bool
	}{
		{
			name:           "successful fetch",
			endpoint:       "http://example.com",
			mockStatusCode: http.StatusOK,
			mockResponse: RegistrationResponse{
				Registration: Registration{
					OrgID:     "org1",
					AblyToken: "ably123",
					Ca:        "ca1",
				},
			},
			expectedResult: &Registration{
				OrgID:     "org1",
				AblyToken: "ably123",
				Ca:        "ca1",
			},
			expectError: false,
		},
		{
			name:           "HTTP error response",
			endpoint:       "http://example.com",
			mockStatusCode: http.StatusInternalServerError,
			mockError:      errors.New("internal server error"),
			expectError:    true,
		},
	}

	// Run the tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockClient := mocks.NewMockHttpClient(ctrl)

			// Set up the mock responses\
			url := tc.endpoint + "/deploy/registration"
			mockClient.EXPECT().GetJSON(url, gomock.Any()).DoAndReturn(func(url string, regResponse *RegistrationResponse) (int, error) {
				*regResponse = tc.mockResponse
				return tc.mockStatusCode, tc.mockError
			})

			// Call the function under test
			result, err := FetchRegistration(mockClient, tc.endpoint)

			// Assert the results
			if tc.expectError {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedResult, result)
			}
		})
	}
}
