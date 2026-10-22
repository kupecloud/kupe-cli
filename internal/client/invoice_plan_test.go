package client

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestListInvoices(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tenants/acme/invoices" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"items":[
		  {"name":"acme-20260301","status":{"phase":"Paid","total":"100.00","currency":"GBP"}},
		  {"name":"acme-20260201","status":{"phase":"Paid","total":"90.00","currency":"GBP"}}
		]}`)
	})
	list, err := c.ListInvoices(context.Background())
	if err != nil {
		t.Fatalf("ListInvoices: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 invoices, got %d", len(list))
	}
	if list[0].Status.Total != "100.00" {
		t.Fatalf("totals not decoded: %+v", list[0])
	}
}

func TestGetInvoiceNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"invoice not found"}`)
	})
	_, err := c.GetInvoice(context.Background(), "ghost")
	if !IsNotFound(err) {
		t.Fatalf("want IsNotFound, got %v", err)
	}
}

func TestListPlansPublicPath(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Plans endpoint is NOT under /tenants/{tenant}.
		if r.URL.Path != "/api/v1/plans" {
			t.Errorf("plans endpoint wrong: %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"items":[
		  {"name":"starter","displayName":"Starter","maxClusters":2},
		  {"name":"pro","displayName":"Pro","maxClusters":5,"platformFee":"49.00"}
		]}`)
	})
	list, err := c.ListPlans(context.Background())
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d, want 2", len(list))
	}
}

func TestGetPlan(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/plans/pro" {
			t.Errorf("plan path = %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"name":"pro","displayName":"Pro","platformFee":"49.00","maxClusters":5,"observabilityPool":{"maxActiveSeries":50000,"logIngestGB":50,"retentionDays":90,"maxReceivers":10}}`)
	})
	p, err := c.GetPlan(context.Background(), "pro")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.ObservabilityPool == nil || p.ObservabilityPool.MaxActiveSeries != 50000 {
		t.Fatalf("ObservabilityPool not decoded: %+v", p)
	}
}

// TestGetPlan_DecodesDatabaseLimit keeps an explicit 0 (unlimited) apart
// from a limit the server does not report (an older kupe-api), which
// decodes as nil.
func TestGetPlan_DecodesDatabaseLimit(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *int64
	}{
		{name: "limited", body: `{"name":"pro","maxClusters":5,"maxDatabases":5}`, want: new(int64(5))},
		{name: "unlimited", body: `{"name":"enterprise","maxClusters":0,"maxDatabases":0}`, want: new(int64(0))},
		{name: "not reported", body: `{"name":"pro","maxClusters":5}`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, tt.body)
			})
			p, err := c.GetPlan(context.Background(), "pro")
			if err != nil {
				t.Fatalf("GetPlan: %v", err)
			}
			switch {
			case tt.want == nil && p.MaxDatabases != nil:
				t.Errorf("MaxDatabases = %d, want nil", *p.MaxDatabases)
			case tt.want != nil && p.MaxDatabases == nil:
				t.Errorf("MaxDatabases = nil, want %d", *tt.want)
			case tt.want != nil && *p.MaxDatabases != *tt.want:
				t.Errorf("MaxDatabases = %d, want %d", *p.MaxDatabases, *tt.want)
			}
		})
	}
}

// TestGetPlan_DecodesRegistryAllowance reads the registry storage cap from
// the plan's registry block, and leaves Registry nil when the server sends
// none.
func TestGetPlan_DecodesRegistryAllowance(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		want       string
		noRegistry bool
	}{
		{name: "limited", body: `{"name":"pro","maxClusters":5,"registry":{"maxStorage":"5Gi","includedStorage":"1Gi"}}`, want: "5Gi"},
		{name: "unlimited", body: `{"name":"enterprise","maxClusters":0,"registry":{"maxStorage":"0","includedStorage":"0"}}`, want: "0"},
		{name: "no registry", body: `{"name":"starter","maxClusters":2}`, noRegistry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, tt.body)
			})
			p, err := c.GetPlan(context.Background(), "pro")
			if err != nil {
				t.Fatalf("GetPlan: %v", err)
			}
			switch {
			case tt.noRegistry && p.Registry != nil:
				t.Errorf("Registry = %+v, want nil", *p.Registry)
			case !tt.noRegistry && p.Registry == nil:
				t.Errorf("Registry = nil, want maxStorage %q", tt.want)
			case !tt.noRegistry && p.Registry.MaxStorage != tt.want:
				t.Errorf("MaxStorage = %q, want %q", p.Registry.MaxStorage, tt.want)
			}
		})
	}
}
