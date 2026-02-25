package remote

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

// retryDo executes an HTTP request with exponential backoff retry.
// Retries on network errors, HTTP 429, and HTTP 5xx responses.
// Does not retry 4xx client errors.
// For requests with a body, the body is buffered and replayed on retry.
func retryDo(client *http.Client, req *http.Request, maxAttempts int) (*http.Response, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Buffer body for replay on retry.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	var lastResp *http.Response
	var lastErr error
	backoff := time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		// Reset body for each attempt.
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			lastResp = nil
			continue
		}

		// Don't retry client errors (4xx) except 429.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// Don't retry success.
		if resp.StatusCode < 400 {
			return resp, nil
		}

		// Retryable: 429 or 5xx. Drain and close body before retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastResp = resp
		lastErr = nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}

// isRetryableStatus returns true for HTTP status codes that should be retried.
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
