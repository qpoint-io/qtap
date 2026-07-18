package httpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"

	"gopkg.in/yaml.v3"
)

//go:generate go tool go.uber.org/mock/mockgen -destination ./mocks/http_client_mock.go -package mocks . HttpClient
type HttpClient interface {
	Get(url string) (io.ReadCloser, int, error)
	GetJSON(url string, out any) (int, error)
	GetYAML(url string, out any) (int, error)
	PostJSON(url string, payload any, out any) (int, error)
}

// Client is an opinionated http Client used within qpoint for
// making calls to different qpoint resources. It holds a
// token which, if present, is included as a header:
// Authorization: Bearer <token>
// and takes a pointers to unmarshal json responses too.
// If the pointer is nil, it will not try to unmarshal.
type Client struct {
	token  string
	client *http.Client
}

func New(token string) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100

	return &Client{
		token: token,
		client: &http.Client{
			Transport: t,
		},
	}
}

func (c *Client) GetJSON(url string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return -1, fmt.Errorf("creating request: %w", err)
	}

	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if out != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, err
		}

		err = json.Unmarshal(body, &out)
		if err != nil {
			return resp.StatusCode, fmt.Errorf("unmarshaling body \"%s\", error: %w", string(body), err)
		}
	}

	return resp.StatusCode, nil
}

func (c *Client) GetYAML(url string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return -1, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Add("Accept", "application/yaml")
	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if out != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, err
		}

		err = yaml.Unmarshal(body, out)
		if err != nil {
			return resp.StatusCode, fmt.Errorf("unmarshaling body \"%s\", error: %w", string(body), err)
		}
	}

	return resp.StatusCode, nil
}

func (c *Client) Get(url string) (io.ReadCloser, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, -1, fmt.Errorf("creating request: %w", err)
	}

	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("making request: %w", err)
	}
	// defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, errors.New("bad status code")
	}

	return resp.Body, resp.StatusCode, nil
}

func (c *Client) PostJSON(url string, in any, out any) (int, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return -1, fmt.Errorf("marshalling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return -1, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")

	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		respDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return -1, err
		}
		return resp.StatusCode, fmt.Errorf("%s error from API, response dump: %s", resp.Status, string(respDump))
	}

	if out != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, err
		}

		err = json.Unmarshal(body, &out)
		if err != nil {
			return resp.StatusCode, err
		}
	}

	return resp.StatusCode, nil
}

func (c *Client) PostJSONRaw(url string, in []byte, out any) (int, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(in))
	if err != nil {
		return -1, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")

	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		respDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return -1, err
		}
		return resp.StatusCode, fmt.Errorf("%s error from API, response dump: %s", resp.Status, string(respDump))
	}

	if out != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp.StatusCode, err
		}

		err = json.Unmarshal(body, &out)
		if err != nil {
			return resp.StatusCode, err
		}
	}

	return resp.StatusCode, nil
}

func (c *Client) SetToken(token string) {
	c.token = token
}

func (c *Client) GetToken() string {
	return c.token
}
