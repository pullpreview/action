package license

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Result struct {
	State   string
	Message string
}

func (r Result) OK() bool {
	return r.State == "ok"
}

type Client struct {
	BaseURL string
}

func New() *Client {
	return &Client{BaseURL: "https://app.pullpreview.com"}
}

func (c *Client) Check(params map[string]string) (Result, error) {
	endpoint := c.BaseURL + "/licenses/check"
	u, err := url.Parse(endpoint)
	if err != nil {
		return Result{State: "ok", Message: "License server unreachable. Continuing..."}, nil
	}
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return Result{State: "ok", Message: "License server unreachable. Continuing..."}, nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{State: "ok", Message: fmt.Sprintf("License server unreachable. Continuing...")}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return Result{State: "ok", Message: string(body)}, nil
	}
	return Result{State: "ko", Message: string(body)}, nil
}
