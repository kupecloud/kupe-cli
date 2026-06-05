package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxResponseBody caps any single HTTP response body we'll read into memory.
// Matches the tf-provider client's limit — protects against a pathological
// server streaming unbounded data at us.
const maxResponseBody = 10 << 20 // 10 MiB

// requestTimeout is the per-attempt deadline. The retry loop may make
// several attempts, each capped at this value.
const requestTimeout = 30 * time.Second

// Client is the authenticated HTTP client for one tenant. Concurrent use by
// multiple goroutines is safe: httpClient and all fields are immutable after
// construction.
type Client struct {
	baseURL    string
	tenant     string
	token      string
	userAgent  string
	httpClient *http.Client
	retry      retryPolicy
	// trace, if non-nil, is called once per HTTP round-trip with the
	// method, path, status code, duration, and request-id. Tokens and
	// bodies are NEVER passed to trace. Wired from --verbose / -v.
	trace TraceFunc
}

// TraceFunc is called once per HTTP round-trip when request logging is
// enabled. requestID may be empty if the server didn't emit one.
type TraceFunc func(method, path string, status int, duration time.Duration, requestID string)

// Option tweaks a Client at construction. Currently used for injecting a
// test HTTP client; more knobs land in later phases if needed.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client. Tests pass an
// httptest-backed client here.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithRetryPolicy overrides the default retry policy. Tests use this to
// shrink backoff to zero for speed.
func WithRetryPolicy(p retryPolicy) Option {
	return func(c *Client) { c.retry = p }
}

// WithTrace installs a TraceFunc called once per HTTP round-trip. The
// function receives no secrets (no Authorization header, no request/
// response body) — so it's safe to wire through to stderr logging when
// --verbose is set.
func WithTrace(fn TraceFunc) Option {
	return func(c *Client) { c.trace = fn }
}

// New builds a Client. baseURL should be the API root (no trailing slash);
// userAgent identifies the caller in logs. tenant and token are typically
// mandatory but can be empty when the client is only used for unauthenticated
// endpoints such as /api/v1/plans (see factory.PublicClient).
func New(baseURL, tenant, token, userAgent string, opts ...Option) *Client {
	c := &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		tenant:    tenant,
		token:     token,
		userAgent: userAgent,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		retry: defaultRetryPolicy,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Tenant returns the tenant this client is scoped to.
func (c *Client) Tenant() string { return c.tenant }

// BaseURL returns the configured API base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// request is a GET/DELETE convenience wrapper that doesn't send a body.
func (c *Client) request(ctx context.Context, method, path string, body, result any) (string, error) {
	return c.requestWithETag(ctx, method, path, "", body, result)
}

// requestWithETag is the workhorse: marshal body, execute with retry,
// decode into result, surface typed errors.
func (c *Client) requestWithETag(ctx context.Context, method, path, etag string, body, result any) (string, error) {
	var bodyBytes []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("marshalling request body: %w", err)
		}
		bodyBytes = data
	}

	resp, respBody, err := c.doWithRetry(ctx, method, path, etag, bodyBytes)
	if err != nil {
		return "", err
	}

	requestID := resp.Header.Get("X-Request-Id")

	if resp.StatusCode >= 400 {
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(respBody)),
			RequestID:  requestID,
			Method:     method,
			Path:       path,
		}
		// Parse the unified envelope: structured canonical responses carry
		// a `code` field; legacy responses only carry `error`. The same
		// struct handles both — we just decide which fields to surface.
		// On JSON parse failure we keep the raw body as Message above so
		// users still see *something* (an HTML 502 page, a proxy error
		// string) instead of "".
		var env errorEnvelope
		if json.Unmarshal(respBody, &env) == nil {
			switch {
			case env.Code != "":
				apiErr.Code = env.Code
				apiErr.Severity = env.Severity
				apiErr.Field = env.Field
				// Prefer Message; fall back to the duplicated Error field;
				// keep the raw body as the last resort.
				switch {
				case env.Message != "":
					apiErr.Message = env.Message
				case env.Error != "":
					apiErr.Message = env.Error
				}
			case env.Error != "":
				apiErr.Message = env.Error
			case env.Message != "":
				// Defensive: a server that returns `message` without `code`
				// shouldn't happen today, but we honour it rather than
				// dropping a useful string on the floor.
				apiErr.Message = env.Message
			}
		}
		return "", apiErr
	}

	respETag := resp.Header.Get("ETag")
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return "", fmt.Errorf("decoding response: %w", err)
		}
	}
	return respETag, nil
}

// doWithRetry executes a request with retry + 429 handling. The response
// body is fully read and closed before return; the caller receives both
// the *http.Response (for headers/status) and the body bytes.
func (c *Client) doWithRetry(ctx context.Context, method, path, etag string, bodyBytes []byte) (*http.Response, []byte, error) {
	var (
		lastResp   *http.Response
		lastBody   []byte
		lastErr    error
		retried429 bool
	)

	for attempt := 0; attempt < c.retry.maxAttempts; attempt++ {
		req, err := c.newRequest(ctx, method, path, etag, bodyBytes)
		if err != nil {
			return nil, nil, err
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		var body []byte
		if resp != nil {
			var readErr error
			body, readErr = io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
			_ = resp.Body.Close()
			// Surface a mid-transfer read failure rather than handing the
			// caller a partial body that will fail later with a confusing
			// "unexpected end of JSON input".
			if readErr != nil && err == nil {
				err = fmt.Errorf("reading response body: %w", readErr)
			}
		}
		if c.trace != nil {
			status := 0
			reqID := ""
			if resp != nil {
				status = resp.StatusCode
				reqID = resp.Header.Get("X-Request-Id")
			}
			c.trace(method, path, status, time.Since(start), reqID)
		}
		lastResp, lastBody, lastErr = resp, body, err

		// 429 — retry once with Retry-After, regardless of method.
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests && !retried429 {
			retried429 = true
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			if wait <= 0 {
				wait = defaultRetryAfter
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, nil, err
			}
			continue
		}

		// Classic retry eligibility (5xx, network errors, idempotent only).
		if !c.retry.shouldRetry(method, resp, err) {
			break
		}
		if attempt+1 >= c.retry.maxAttempts {
			break
		}
		if err := sleepCtx(ctx, c.retry.backoff(attempt)); err != nil {
			return nil, nil, err
		}
	}

	if lastErr != nil && lastResp == nil {
		return nil, nil, fmt.Errorf("executing request: %w", lastErr)
	}
	return lastResp, lastBody, nil
}

// newRequest builds a fresh *http.Request — called once per attempt so
// retries get an unconsumed body reader.
func (c *Client) newRequest(ctx context.Context, method, path, etag string, bodyBytes []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	reqURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Token is optional: the public plan catalog (/api/v1/plans) accepts
	// unauthenticated calls, so clients built for that endpoint pass "".
	// Sending "Bearer " with an empty secret would trip the server's auth
	// middleware needlessly — skip the header instead.
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	return req, nil
}

// tenantPath builds a tenant-scoped API path with URL escaping.
func (c *Client) tenantPath(segments ...string) string {
	p := "/api/v1/tenants/" + url.PathEscape(c.tenant)
	for _, s := range segments {
		p += "/" + url.PathEscape(s)
	}
	return p
}
