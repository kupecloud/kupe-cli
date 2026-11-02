package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestImageResolveDisplaysMetadataAndRawJSON(t *testing.T) {
	for _, output := range []string{"table", "json"} {
		fake := clienttest.New()
		fake.ImageResolution = &client.ImageResolution{Reference: nextDigest, User: "65532", ExposedPorts: []int32{8080}, Platforms: []client.ImagePlatform{{OS: "linux", Architecture: "amd64", Digest: "sha256:platform"}}}
		out, _, err := run(factoryWith(fake), "image", "resolve", "hello-kupe:release", "-o", output)
		if err != nil || len(fake.ResolvedImages) != 1 || fake.ResolvedImages[0] != "hello-kupe:release" {
			t.Fatalf("out=%s err=%v calls=%v", out, err, fake.Calls)
		}
		if output == "json" {
			var got client.ImageResolution
			if err := json.Unmarshal([]byte(out), &got); err != nil || got.Reference != nextDigest || got.User != "65532" || got.ExposedPorts[0] != 8080 {
				t.Fatalf("out=%s err=%v", out, err)
			}
		} else if !strings.Contains(out, nextDigest) || !strings.Contains(out, "linux/amd64") || !strings.Contains(out, "65532") || !strings.Contains(out, "8080") {
			t.Fatal(out)
		}
	}
}

func TestImageResolveExplainsEachRefusalAndRetryAfterWithoutRawData(t *testing.T) {
	for _, tc := range []struct {
		code         string
		status, exit int
		message      string
	}{
		{"image_not_found", 404, 4, "not found or not accessible"},
		{"image_unavailable", 503, 7, "temporarily unavailable"},
		{"image_platform_unsupported", 400, 2, "linux/amd64"},
		{"image_policy_denied", 400, 2, "non-root numeric USER"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			fake := clienttest.New()
			fake.ResolveAppImageErr = &client.APIError{StatusCode: tc.status, Code: tc.code, Message: "synthetic-upstream-body", RequestID: "safe-image-id", RetryAfter: time.Minute}
			out, stderr, err := run(factoryWith(fake), "image", "resolve", "hello:release")
			if cli.ExitCode(err) != tc.exit || !strings.Contains(err.Error(), tc.message) || strings.Contains(out+stderr+err.Error(), "synthetic-upstream-body") || !strings.Contains(err.Error(), "safe-image-id") {
				t.Fatalf("err=%v out=%s stderr=%s", err, out, stderr)
			}
			var ce *cli.Error
			if tc.code == "image_unavailable" && (!errors.As(err, &ce) || !strings.Contains(ce.Hint, "1m0s")) {
				t.Fatalf("hint=%+v", ce)
			}
		})
	}
}

func TestImageInputRefusalsAndInvalidResolverAnswer(t *testing.T) {
	for _, image := range []string{"", strings.Repeat("a", 513), "repo:tag\nforged", "https://user:synthetic@registry.example/image:tag"} {
		fake := clienttest.New()
		_, _, err := run(factoryWith(fake), "image", "resolve", image)
		if cli.ExitCode(err) != 2 || len(fake.Calls) != 0 {
			t.Fatalf("err=%v calls=%v", err, fake.Calls)
		}
	}
	fake := clienttest.New()
	fake.ImageResolution = &client.ImageResolution{Reference: "mutable:tag"}
	_, _, err := run(factoryWith(fake), "image", "resolve", "image:tag")
	if cli.ExitCode(err) != 7 {
		t.Fatalf("invalid resolver digest accepted: %v", err)
	}
}

func TestImageCommandHonoursRollingHourBudgetHintFromHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/app-images/resolve") {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"code":"image_unavailable","message":"synthetic-upstream-body"}`))
	}))
	defer server.Close()
	f := factoryWith(clienttest.New())
	f.Client = func() (client.Interface, error) { return client.New(server.URL, "acme", "", "test"), nil }
	out, stderr, err := run(f, "image", "resolve", "hello:release")
	var ce *cli.Error
	if cli.ExitCode(err) != 7 || !errors.As(err, &ce) || !strings.Contains(ce.Hint, "1h0m0s") || strings.Contains(out+stderr+err.Error(), "synthetic-upstream-body") {
		t.Fatalf("out=%s stderr=%s err=%v", out, stderr, err)
	}
}
