package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RunRequestAction(ctx context.Context, payload RequestActionPayload) (*RequestActionResult, error) {
	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(payload.URL), strings.NewReader(payload.Body))
	if err != nil {
		return nil, err
	}
	for _, header := range payload.Headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) != 2 {
			continue
		}
		req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var headers strings.Builder
	headers.WriteString(fmt.Sprintf("HTTP/%d.%d %d %s\r\n", resp.ProtoMajor, resp.ProtoMinor, resp.StatusCode, http.StatusText(resp.StatusCode)))
	for name, values := range resp.Header {
		for _, value := range values {
			headers.WriteString(name)
			headers.WriteString(": ")
			headers.WriteString(value)
			headers.WriteString("\r\n")
		}
	}

	return &RequestActionResult{
		URL:        req.URL.String(),
		Method:     method,
		StatusCode: resp.StatusCode,
		Headers:    headers.String(),
		Body:       string(body),
	}, nil
}
