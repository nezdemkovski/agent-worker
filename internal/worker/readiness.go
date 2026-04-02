package worker

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type ProbeResult struct {
	StatusCode int
	Headers    string
	Body       string
}

func (p ProbeResult) Ready() bool {
	return p.StatusCode >= 200 && p.StatusCode < 400
}

func probeReady(url string) (*ProbeResult, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var headers bytes.Buffer
	fmt.Fprintf(&headers, "%s %s\r\n", resp.Proto, resp.Status)
	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range resp.Header.Values(key) {
			fmt.Fprintf(&headers, "%s: %s\r\n", key, value)
		}
	}

	return &ProbeResult{
		StatusCode: resp.StatusCode,
		Headers:    headers.String(),
		Body:       string(body),
	}, nil
}
