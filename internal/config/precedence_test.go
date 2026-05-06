package config

import "testing"

func TestResolvePrecedence(t *testing.T) {
	cfg := &Config{
		CurrentContext: "prod",
		Contexts: []Context{
			{Name: "prod", APIURL: "https://api.kupe.cloud", Tenant: "acme", TokenRef: TokenRefKeyring},
		},
	}

	tests := []struct {
		name    string
		flags   Flags
		env     Env
		wantURL string
		wantTen string
		wantCtx string
		wantTok string
	}{
		{
			name:    "pure-config",
			wantURL: "https://api.kupe.cloud",
			wantTen: "acme",
			wantCtx: "prod",
		},
		{
			name:    "flag-overrides-env-and-config",
			flags:   Flags{APIURL: "https://flag.example", Tenant: "flag-ten", Token: "flag-tok"},
			env:     Env{APIURL: "https://env.example", Tenant: "env-ten", Token: "env-tok"},
			wantURL: "https://flag.example",
			wantTen: "flag-ten",
			wantCtx: "prod",
			wantTok: "flag-tok",
		},
		{
			name:    "env-overrides-config",
			env:     Env{APIURL: "https://env.example", Tenant: "env-ten", Token: "env-tok"},
			wantURL: "https://env.example",
			wantTen: "env-ten",
			wantCtx: "prod",
			wantTok: "env-tok",
		},
		{
			name:    "context-switch-via-flag",
			flags:   Flags{Context: "missing"}, // name present but context absent
			wantURL: DefaultAPIURL,             // no matching context → default
			wantTen: "",                        // no tenant resolvable
			wantCtx: "missing",
		},
		{
			name:    "default-url-when-nothing-set",
			flags:   Flags{Tenant: "standalone"},
			wantURL: DefaultAPIURL,
			wantTen: "standalone",
			wantCtx: "prod", // inherits from cfg.CurrentContext
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolve(tt.flags, tt.env, cfg)
			if r.APIURL != tt.wantURL {
				t.Errorf("APIURL = %q; want %q", r.APIURL, tt.wantURL)
			}
			if r.Tenant != tt.wantTen {
				t.Errorf("Tenant = %q; want %q", r.Tenant, tt.wantTen)
			}
			if r.ContextName != tt.wantCtx {
				t.Errorf("ContextName = %q; want %q", r.ContextName, tt.wantCtx)
			}
			if r.DirectToken != tt.wantTok {
				t.Errorf("DirectToken = %q; want %q", r.DirectToken, tt.wantTok)
			}
		})
	}
}

func TestResolveWithNilConfig(t *testing.T) {
	r := Resolve(
		Flags{Tenant: "standalone"},
		Env{APIURL: "https://env.example"},
		nil,
	)
	if r.Tenant != "standalone" {
		t.Fatalf("Tenant = %q; want standalone", r.Tenant)
	}
	if r.APIURL != "https://env.example" {
		t.Fatalf("APIURL = %q; want env override", r.APIURL)
	}
	if r.ContextName != "" {
		t.Fatalf("ContextName = %q; want empty when no config + no flag/env context", r.ContextName)
	}
}

func TestBuildIssuerURL(t *testing.T) {
	cases := []struct {
		base, clientID, want string
	}{
		{"https://auth.kupe.cloud", "kupe-cli", "https://auth.kupe.cloud/application/o/kupe-cli/"},
		{"https://auth.kupe.cloud/", "kupe-cli", "https://auth.kupe.cloud/application/o/kupe-cli/"},
		{"https://auth.dev.int.kupe.cloud//", "other-app", "https://auth.dev.int.kupe.cloud/application/o/other-app/"},
		{"", "kupe-cli", ""},
		{"https://auth.kupe.cloud", "", ""},
	}
	for _, tt := range cases {
		got := BuildIssuerURL(tt.base, tt.clientID)
		if got != tt.want {
			t.Errorf("BuildIssuerURL(%q, %q) = %q; want %q", tt.base, tt.clientID, got, tt.want)
		}
	}
}

func TestResolveComputesOIDCIssuerFromBase(t *testing.T) {
	cfg := &Config{
		CurrentContext: "dev",
		Contexts: []Context{
			{Name: "dev", APIURL: "https://api.dev.int.kupe.cloud", Tenant: "kupe-test",
				OIDCBaseURL: "https://auth.dev.int.kupe.cloud"},
		},
	}
	r := Resolve(Flags{}, Env{}, cfg)
	if r.OIDCBaseURL != "https://auth.dev.int.kupe.cloud" {
		t.Errorf("OIDCBaseURL = %q", r.OIDCBaseURL)
	}
	if r.OIDCIssuer != "https://auth.dev.int.kupe.cloud/application/o/kupe-cli/" {
		t.Errorf("OIDCIssuer = %q", r.OIDCIssuer)
	}
}

func TestResolveOIDCEnvOverridesContext(t *testing.T) {
	cfg := &Config{
		CurrentContext: "dev",
		Contexts: []Context{
			{Name: "dev", Tenant: "x", OIDCBaseURL: "https://ctx.example"},
		},
	}
	r := Resolve(Flags{}, Env{OIDCBaseURL: "https://env.example", OIDCClientID: "alt-app"}, cfg)
	if r.OIDCIssuer != "https://env.example/application/o/alt-app/" {
		t.Fatalf("OIDCIssuer = %q; want env-derived", r.OIDCIssuer)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"api.kupe.cloud", "https://api.kupe.cloud"},
		{"https://api.kupe.cloud", "https://api.kupe.cloud"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"api.kupe.cloud:8443", "https://api.kupe.cloud:8443"},
	}
	for _, tt := range cases {
		if got := NormalizeURL(tt.in); got != tt.want {
			t.Errorf("NormalizeURL(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidateKupeURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty accepted", "", false},
		{"prod api", "https://api.kupe.cloud", false},
		{"dev internal", "https://api.dev.int.kupe.cloud", false},
		{"bare kupe.cloud", "https://kupe.cloud", false},
		{"http scheme on kupe.cloud", "http://api.kupe.cloud", false},
		{"with port", "https://api.kupe.cloud:8443", false},
		{"localhost rejected", "http://localhost:8080", true},
		{"127.0.0.1 rejected", "http://127.0.0.1:8080", true},
		{"foreign domain rejected", "https://api.evil.com", true},
		{"typo rejected", "https://api.kup.cloud", true},
		{"sub of foreign rejected", "https://kupe.cloud.evil.com", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKupeURL(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKupeURL(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}
