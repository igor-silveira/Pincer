package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func doLLMRequest(ctx context.Context, client *http.Client, providerName, url string, headers map[string]string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshaling request: %w", providerName, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: creating request: %w", providerName, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: sending request: %w", providerName, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: API returned %d: %s", providerName, resp.StatusCode, string(errBody))
	}

	return resp, nil
}

func dispatchResponse(resp *http.Response, stream bool, readStream func(io.ReadCloser, chan<- ChatEvent), readFull func(io.ReadCloser, chan<- ChatEvent)) <-chan ChatEvent {
	ch := make(chan ChatEvent, 64)
	if stream {
		go readStream(resp.Body, ch)
	} else {
		go readFull(resp.Body, ch)
	}
	return ch
}
