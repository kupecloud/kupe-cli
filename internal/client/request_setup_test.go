package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRequestNotSentPreservesSetupCauses(t *testing.T) {
	cause := errors.New("synthetic token failure")
	for _, test := range []struct {
		name string
		call func(*Client) error
	}{
		{"token", func(c *Client) error {
			c.tokenSource = func(context.Context) (string, error) { return "", cause }
			_, err := c.PutVaultValues(context.Background(), "team/db", map[string]string{"A": "synthetic"}, 0)
			return err
		}},
		{"construction", func(c *Client) error {
			c.baseURL = ":invalid"
			_, err := c.PutVaultValues(context.Background(), "team/db", map[string]string{"A": "synthetic"}, 0)
			return err
		}},
		{"marshal", func(c *Client) error {
			_, err := c.request(context.Background(), http.MethodPost, "/synthetic", make(chan int), nil)
			return err
		}},
		{"cancel-before-request", func(c *Client) error {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := c.PutVaultValues(ctx, "team/db", map[string]string{"A": "synthetic"}, 0)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			return err
		}},
		{"cancel-during-token", func(c *Client) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.tokenSource = func(context.Context) (string, error) { cancel(); return "synthetic", nil }
			_, err := c.PutVaultValues(ctx, "team/db", map[string]string{"A": "synthetic"}, 0)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			c := New("https://synthetic.invalid", "acme", "", "test", WithHTTPClient(&http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				return nil, errors.New("unexpected transport")
			})}))
			err := test.call(c)
			var setup *RequestNotSentError
			if !errors.As(err, &setup) || attempts != 0 {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
			if test.name == "token" && (!setup.TokenResolution || !errors.Is(err, cause)) {
				t.Fatalf("token cause lost: %v", err)
			}
		})
	}
}

func TestSetupFailureAfterAttemptOnlyUnsentAfterLimiterRefusal(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			tokens, attempts := 0, 0
			cause := errors.New("synthetic expired token")
			c := New("https://synthetic.invalid", "acme", "", "test",
				WithRetryPolicy(fastRetry),
				WithTokenSource(func(context.Context) (string, error) {
					tokens++
					if tokens > 1 {
						return "", cause
					}
					return "synthetic", nil
				}),
				WithHTTPClient(&http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
					attempts++
					return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"0.001"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				})}),
			)
			// Ordinary DELETE may retry a 502; its earlier outcome must not be
			// hidden by local setup failing on the next attempt.
			_, err := c.request(context.Background(), http.MethodDelete, "/synthetic", nil, nil)
			var setup *RequestNotSentError
			unsent := errors.As(err, &setup)
			if attempts != 1 || tokens != 2 || !errors.Is(err, cause) || unsent != (status == http.StatusTooManyRequests) {
				t.Fatalf("attempts=%d tokens=%d unsent=%v err=%v", attempts, tokens, unsent, err)
			}
		})
	}
}

func TestVaultSubmittedFailuresAreNeverNotSent(t *testing.T) {
	for _, failure := range []string{"transport", "body"} {
		t.Run(failure, func(t *testing.T) {
			attempts := 0
			c := New("https://synthetic.invalid", "acme", "", "test", WithHTTPClient(&http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				if failure == "transport" {
					return nil, context.Canceled
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: errBody{}}, nil
			})}))
			_, err := c.PutVaultValues(context.Background(), "team/db", map[string]string{"A": "synthetic"}, 0)
			var setup *RequestNotSentError
			if err == nil || errors.As(err, &setup) || attempts != 1 {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
}
