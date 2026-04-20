package config

import "testing"

func TestContextLookup(t *testing.T) {
	cfg := &Config{Contexts: []Context{
		{Name: "a", Tenant: "ta"},
		{Name: "b", Tenant: "tb"},
	}}

	if c := cfg.Context("a"); c == nil || c.Tenant != "ta" {
		t.Fatalf("Context(a) = %+v; want {a ta}", c)
	}
	if c := cfg.Context("missing"); c != nil {
		t.Fatalf("Context(missing) = %+v; want nil", c)
	}
}

func TestCurrentCtx(t *testing.T) {
	cfg := &Config{
		CurrentContext: "prod",
		Contexts:       []Context{{Name: "prod", Tenant: "acme"}},
	}
	if c := cfg.CurrentCtx(); c == nil || c.Name != "prod" {
		t.Fatalf("CurrentCtx = %+v; want {prod acme}", c)
	}

	// Empty current → nil.
	cfg.CurrentContext = ""
	if c := cfg.CurrentCtx(); c != nil {
		t.Fatalf("CurrentCtx with empty CurrentContext = %+v; want nil", c)
	}

	// Dangling pointer to missing context → nil, not panic.
	cfg.CurrentContext = "missing"
	if c := cfg.CurrentCtx(); c != nil {
		t.Fatalf("CurrentCtx with missing context = %+v; want nil", c)
	}
}

func TestSetContextAddsAndUpdates(t *testing.T) {
	cfg := New()
	cfg.SetContext(Context{Name: "a", Tenant: "ta"})
	if len(cfg.Contexts) != 1 {
		t.Fatalf("want 1 context, got %d", len(cfg.Contexts))
	}
	cfg.SetContext(Context{Name: "a", Tenant: "ta-updated"})
	if len(cfg.Contexts) != 1 {
		t.Fatalf("replace should not grow slice; got %d", len(cfg.Contexts))
	}
	if cfg.Contexts[0].Tenant != "ta-updated" {
		t.Fatalf("tenant = %s; want ta-updated", cfg.Contexts[0].Tenant)
	}
}

func TestRemoveContextClearsCurrent(t *testing.T) {
	cfg := &Config{
		CurrentContext: "prod",
		Contexts:       []Context{{Name: "prod"}, {Name: "staging"}},
	}
	if !cfg.RemoveContext("prod") {
		t.Fatal("RemoveContext returned false for existing context")
	}
	if cfg.CurrentContext != "" {
		t.Fatalf("CurrentContext = %q; want cleared", cfg.CurrentContext)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts[0].Name != "staging" {
		t.Fatalf("remaining contexts = %+v", cfg.Contexts)
	}

	if cfg.RemoveContext("ghost") {
		t.Fatal("RemoveContext returned true for missing context")
	}
}
