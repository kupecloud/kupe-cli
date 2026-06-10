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
