package register

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/qpoint-io/qtap/pkg/httpclient"
)

type Registration struct {
	OrgID     string `json:"orgId"`
	AblyToken string `json:"ablyToken"`
	Ca        string `json:"ca"`
}

type RegistrationResponse struct {
	Registration Registration `json:"registration"`
}

func FetchRegistration(client httpclient.HttpClient, endpoint string) (*Registration, error) {
	// make the API request
	url := endpoint + "/deploy/registration"

	var registration RegistrationResponse
	status, err := client.GetJSON(url, &registration)
	if err != nil {
		return nil, fmt.Errorf("fetching registration status: '%s' from '%s' error: %w", url, http.StatusText(status), err)
	}

	// check if the response was successful (status code 200)
	if status != http.StatusOK {
		return nil, fmt.Errorf("request failed, status \"%s\"", http.StatusText(status))
	}

	// extract just the registration
	return &registration.Registration, nil
}

func FetchCertificate(client httpclient.HttpClient, endpoint string) (string, error) {
	// make the API request
	url := endpoint + "/deploy/certificate"

	body, status, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching registration certificate status: '%s' from '%s' error: %w", url, http.StatusText(status), err)
	}

	// check if the response was successful (status code 200)
	if status != http.StatusOK {
		return "", fmt.Errorf("request failed, status \"%s\"", http.StatusText(status))
	}

	if body == nil {
		return "", errors.New("expected body but got nil")
	}

	b, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("reading body from ReadCloser: %w", err)
	}

	// extract just the registration
	return string(b), nil
}
