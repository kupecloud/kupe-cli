package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// TestStructuredErrorEnvelopePopulatesAllFields verifies the new canonical
// envelope (code + severity + field + message) is fully decoded onto
// APIError. Existing code that only reads Message still gets the human
// string; new code can branch on Code/Field.
func TestStructuredErrorEnvelopePopulatesAllFields(t *testing.T) {
	body := `{"code":"HA_DISABLE_UNSUPPORTED","severity":"error","message":"Disabling HA is not supported in v1.","field":"spec.highAvailability","error":"Disabling HA is not supported in v1."}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "req-struct")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, body)
	})

	_, _, err := c.GetTenant(context.Background())
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T", err)
	}
	if apiErr.Code != "HA_DISABLE_UNSUPPORTED" {
		t.Errorf("Code = %q; want HA_DISABLE_UNSUPPORTED", apiErr.Code)
	}
	if apiErr.Severity != "error" {
		t.Errorf("Severity = %q; want error", apiErr.Severity)
	}
	if apiErr.Field != "spec.highAvailability" {
		t.Errorf("Field = %q; want spec.highAvailability", apiErr.Field)
	}
	if apiErr.Message != "Disabling HA is not supported in v1." {
		t.Errorf("Message = %q; want the structured message", apiErr.Message)
	}
}

// TestStructuredEnvelopePrefersMessageOverError verifies we pick `message`
// when both are present (newer field), keeping `error` as a fallback only.
func TestStructuredEnvelopePrefersMessageOverError(t *testing.T) {
	body := `{"code":"CLUSTER_DEDICATED_UNSUPPORTED","severity":"error","message":"NEW message","field":"spec.type","error":"OLD message"}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, body)
	})

	_, _, err := c.GetTenant(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Message != "NEW message" {
		t.Errorf("Message = %q; want NEW message (message field preferred)", apiErr.Message)
	}
}

// TestLegacyErrorEnvelopeStillPopulatesMessage guards backward compatibility:
// kupe-api still uses {"error":"..."} for non-canonical 4xx responses, and
// the CLI must keep rendering a useful message with empty Code/Field.
func TestLegacyErrorEnvelopeStillPopulatesMessage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"tenant not found"}`)
	})

	_, _, err := c.GetTenant(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Message != "tenant not found" {
		t.Errorf("Message = %q; want tenant not found", apiErr.Message)
	}
	if apiErr.Code != "" || apiErr.Field != "" || apiErr.Severity != "" {
		t.Errorf("structured fields should be empty for legacy envelope: %+v", apiErr)
	}
}

// TestGarbageErrorBodyFallsBackToRawString covers the contract: if the 4xx
// response isn't valid JSON (proxy 502 HTML, plain text, etc.), Message gets
// the raw body — never the empty string.
func TestGarbageErrorBodyFallsBackToRawString(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html><body>upstream timeout</body></html>")
	})

	_, _, err := c.GetTenant(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Message != "<html><body>upstream timeout</body></html>" {
		t.Errorf("Message = %q; want raw body", apiErr.Message)
	}
}

// TestValidJSONButUnknownShapeFallsBackToRaw covers the "valid JSON but
// neither shape" branch from the task — we keep the raw body string so users
// see *something*, never "".
func TestValidJSONButUnknownShapeFallsBackToRaw(t *testing.T) {
	body := `{"unexpected":"shape","items":[]}`
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, body)
	})

	_, _, err := c.GetTenant(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Message != body {
		t.Errorf("Message = %q; want raw body %q", apiErr.Message, body)
	}
}

// TestClusterWarningsUnmarshal verifies the new Warnings field decodes
// correctly from a successful create response. The API always sends
// `warnings` (empty array when none); we want both the populated and empty
// case to work without surprises.
func TestClusterWarningsUnmarshal(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
			"name":"prod",
			"displayName":"Prod",
			"type":"shared",
			"version":"1.33",
			"resourceVersion":"1",
			"createdAt":"2026-06-05T00:00:00Z",
			"warnings":[
				{"code":"HA_K8S_VERSION_RETIRING","severity":"warning","message":"K8s 1.33 is approaching EOL.","field":"spec.highAvailability"}
			]
		}`)
	})

	cluster, _, err := c.CreateCluster(context.Background(), CreateClusterRequest{Name: "prod", Type: "shared", DisplayName: "Prod"})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if len(cluster.Warnings) != 1 {
		t.Fatalf("Warnings len = %d; want 1", len(cluster.Warnings))
	}
	w := cluster.Warnings[0]
	if w.Code != "HA_K8S_VERSION_RETIRING" || w.Severity != "warning" || w.Field != "spec.highAvailability" || w.Message == "" {
		t.Errorf("warning = %+v; not the expected shape", w)
	}
}

// TestClusterWarningsEmpty exercises the always-present empty array case.
func TestClusterWarningsEmpty(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"name":"prod","type":"shared","resourceVersion":"1","createdAt":"2026-06-05T00:00:00Z","warnings":[]}`)
	})

	cluster, _, err := c.CreateCluster(context.Background(), CreateClusterRequest{Name: "prod", Type: "shared", DisplayName: "Prod"})
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if len(cluster.Warnings) != 0 {
		t.Errorf("Warnings = %+v; want empty", cluster.Warnings)
	}
}

// TestWarningRoundTrip sanity-checks JSON tags so a struct change later
// can't silently break the wire format.
func TestWarningRoundTrip(t *testing.T) {
	in := Warning{Code: "X", Severity: "warning", Message: "m", Field: "spec.f"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Warning
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round trip mismatch: %+v vs %+v", in, out)
	}
}
