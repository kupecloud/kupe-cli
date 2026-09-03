---
title: "Testing"
description: "Factory + IOStreams pattern, fake client, printer tests, httptest fixtures, and live-test strategy for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 8
---

The CLI's test strategy has four layers, each owning a specific failure class. `make test` runs the unit, printer, and client layers under `./internal/...`; live tests are separate and gated behind the `live` build tag.

| Layer | Scope | Purpose | Runs in CI? |
|-------|-------|---------|-------------|
| 1. Command unit tests | `internal/cmd/...` | Flag parsing, rendering, error mapping, exit codes | Yes |
| 2. Printer tests | `internal/printer/...` | Output parser/table/detail rendering behavior | Yes |
| 3. Client tests | `internal/client/...` | HTTP plumbing, retries, ETag, error classification | Yes |
| 4. Live tests | `test/live/...` | Full round-trip against a real `kupe-api` | Manual / on-demand |

The single goal: **changes to a command body fail a command test; changes to rendering behavior fail printer tests; changes to API contract fail client or live tests.** Each break has one obvious place to look.

## Layer 1 — command unit tests

### Pattern: Factory + IOStreams

Every command body accepts a `*cli.Factory`. Tests construct a factory backed by in-memory IOStreams and fakes:

```go
func TestClusterList_JSON(t *testing.T) {
    fake := clienttest.New()
    fake.Clusters["prod"] = &client.Cluster{
        Name: "prod", Type: "shared",
        Status: &client.ClusterStatus{Phase: client.PhaseRunning},
    }
    f := factoryWith(t, fake)
    cmd := newListCmd(f)
    cmd.SetArgs([]string{"-o", "json"})

    err := cmd.Execute()
    require.NoError(t, err)

    var got []client.Cluster
    require.NoError(t, json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got))
    require.Len(t, got, 1)
    require.Equal(t, "prod", got[0].Name)
}
```

Most command packages define a small local `factoryWith` helper that wires `cli.Test()` streams, a temporary config file, and `internal/client/clienttest.Fake`. Shared fake API behavior lives in `internal/client/clienttest/`.

### What to test

For each command:

1. Happy path at least one `-o` format (usually `json` for parseability).
2. Error path per classification (not-found, unauthorized, validation).
3. Flag interactions (e.g., `--wait=false` returns immediately; `--yes` skips prompt).
4. Argument validation (missing positional → exit 2).
5. TTY-gated behavior (prompt suppressed in non-TTY; `--yes` required).

### What NOT to test here

- HTTP plumbing details — that's Layer 3.
- Output format byte-level shape — that's Layer 2.
- Live API behavior — that's Layer 4.

## Layer 2 — printer tests

Printer tests live in `internal/printer/`. They cover:

- `printer.Parse` for `table`, `wide`, `json`, `yaml`, `name`, `go-template=...`, and `jsonpath=...`.
- Narrow vs wide table behavior.
- Empty table rendering.
- ANSI-colored cell alignment.
- Detail rendering.

There are no golden files in the current tree. If byte-for-byte output fixtures are added later, they should live under `testdata/` next to the package that owns the renderer.

## Layer 3 — client tests

Test the HTTP client against `httptest.NewServer`:

```go
func TestClient_GetCluster_404(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Equal(t, "/api/v1/tenants/acme/clusters/missing", r.URL.Path)
        require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
        w.Header().Set("X-Request-Id", "test-req-123")
        w.WriteHeader(http.StatusNotFound)
        fmt.Fprintln(w, `{"error": "cluster \"missing\" not found"}`)
    }))
    defer srv.Close()

    c := client.New(srv.URL, "acme", "test-token", "kupe-cli/test")
    _, _, err := c.GetCluster(context.Background(), "missing")
    require.True(t, client.IsNotFound(err))
    require.Contains(t, err.Error(), "test-req-123")
}
```

Coverage targets:

- Resource methods for clusters, kubeconfig, API keys, secrets, members, tenant, invoices, and plans.
- Every typed error helper (`IsUnauthorized`, `IsForbidden`, `IsNotFound`, `IsValidation`, `IsConflict`, `IsPreconditionFailed`, `IsRateLimited`, `IsUnavailable`).
- Retry policy: transient 503 retries, persistent 503 fails after N attempts, POST/PATCH never retry.
- `Retry-After` parsing: integer, HTTP-date, missing, malformed.
- ETag RMW: happy path, 412 retry, 412 twice fails.
- Request-ID propagation into errors.

### Imported tests from terraform-provider-kupe

Several client tests started as copies of terraform-provider-kupe tests. Keep extending them in place as CLI-specific behavior lands, especially retry, `Retry-After`, typed helpers, RMW, and public plan endpoints.

## Layer 4 — live `kupe-api` (`make test-live`)

Lives at [`test/live/`](../test/live/). Mirrors the [kupe-api `test/live/` convention](../../kupe-api/test/live/) so engineers can move between repos without re-learning the layout. Go tests with the `//go:build live` tag, run by exec'ing the freshly-compiled `kupe` binary against a deployed API.

```bash
# Required:
export KUPE_API_TOKEN=kupe_…           # admin key on the testing tenant,
                                        # OR an OIDC JWT bearer
# Optional (defaults shown):
export KUPE_API_URL=https://api.dev.int.kupe.cloud
export KUPE_TEST_TENANT=kupe-test
# Slow lifecycle test (5-8 min); off by default:
export KUPE_LIVE_CLUSTER=1

make test-live
```

What runs:

| File | Coverage |
|------|----------|
| `auth_test.go` | `auth whoami` + bad-token exit-3 path |
| `tenant_test.go` | `tenant get` |
| `tenant_delete_test.go` | `tenant delete` local refusals; full deletion of a throwaway tenant gated on `KUPE_LIVE_DELETE_TENANT=<name>` + an owner OIDC token |
| `plan_test.go` | `plan list/get` (unauthenticated) |
| `apikey_test.go` | `apikey create/list/delete` round-trip |
| `secret_test.go` | `secret create/list/get/update/delete` round-trip |
| `member_test.go` | `member list` (no mutation — shared org state) |
| `invoice_test.go` | `invoice list/get` |
| `cluster_test.go` | full lifecycle gated on `KUPE_LIVE_CLUSTER=1` |

Patterns to follow when adding a test:

- One file per noun. Match the kupe-api naming.
- Resources get `uniqueName(prefix)` so reruns and concurrent invocations don't collide.
- Cleanup via `t.Cleanup` registered immediately after creation — survives test failures and is idempotent.
- Use `runCLIJSON(t, &dst, args…)` for happy-path JSON parsing — it adds `-o json`, asserts exit 0, and runs the no-token-leak check.
- Use `runCLI(t, args…)` when asserting on exit code, stderr, or non-JSON stdout.
- Don't print or compare against `apiToken`; the helper does the leak check for you.

**Auth-mode parity (OIDC vs apikey):** the same suite runs unmodified for either token type — the CLI's `--token` / `KUPE_API_TOKEN` path doesn't care if the bearer is a `kupe_...` API key or an OIDC access token. To verify both auth paths end-to-end, run `make test-live` twice: once with an API key and once with an access token from `kupe auth login --method oidc`.

**Manual OIDC smoke test:** the device-code login itself can't be automated in `go test` — it needs a real Authentik to issue a code and a real human to approve. Test it interactively against dev:

```bash
unset KUPE_API_TOKEN
./bin/kupe auth login \
    --tenant kupe-test \
    --api-url https://api.dev.int.kupe.cloud \
    --oidc-base-url https://auth.dev.int.kupe.cloud
./bin/kupe auth whoami        # should report your email + the tenant
./bin/kupe cluster list       # any authenticated noun proves the JWT path
```

The CLI builds the full issuer URL as `{base}/application/o/{client-id}/` — only the base hostname varies between environments.

**Not wired into GitHub Actions** — intentional. Live tests need WireGuard for the private dev API, mutate state in the testing tenant, and would slow PR feedback. Promote to nightly only if/when the operations matter.

### Fake client for command-layer tests

`internal/client/clienttest/` provides an in-memory implementation of the client interface:

```go
type Fake struct {
    Clusters map[string]*client.Cluster
    APIKeys  map[string]*client.APIKey
    // ...
    CreateHook func(*client.Cluster) error  // test injection point
}

var _ client.Interface = (*Fake)(nil)
```

Command unit tests (Layer 1) use this fake, not `httptest`. The fake is itself tested: a round-trip test ensures it stays behaviorally equivalent to a minimal subset of the real server.

## Test matrix for a new command

When adding a new command, the test checklist is:

- [ ] Unit test: happy path, `-o json`.
- [ ] Unit test: not-found error on a dependent resource.
- [ ] Unit test: validation error on missing required flag.
- [ ] Unit test: `--yes` skips prompt; non-TTY without `--yes` fails.
- [ ] Printer coverage for any new columns or output helper behavior.
- [ ] Client test: happy path.
- [ ] Client test: error per classification the API can emit.
- [ ] Commands.md entry.

A one-page "adding a command" guide lives in this repo's `CONTRIBUTING.md`.

## Lint and vuln

Mirror [kupe-api](../../kupe-api/) and [terraform-provider-kupe](../../terraform-provider-kupe/):

```bash
make lint        # golangci-lint run
make gosec       # gosec
make govulncheck # Go vulnerability check
```

Golangci-lint config sits next to the existing kupe repos' `.golangci.yml`. Same enabled linters, same `errcheck` exclusions (context-scoped, deferred `Close()`), same import-ordering rules. Copy rather than inventing a new one.

`govulncheck` should run before releases and on security-sensitive changes. High-severity advisories block release.

## CI workflow summary

`.github/workflows/ci.yaml`:

```yaml
jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.26.3" }
      - run: make vendor
      - run: make lint
      - run: make test                   # layers 1 + 2 + 3
      - run: make gosec
      - run: make govulncheck
      - run: goreleaser release --snapshot --clean --skip=publish
```

`.github/workflows/e2e-live.yaml` (if/when enabled; schedule + `workflow_dispatch`):

```yaml
on:
  schedule: [{cron: "0 4 * * *"}]
  workflow_dispatch: {}
jobs:
  live:
    environment: e2e
    steps:
      - run: make test-live
        env:
          KUPE_API_URL: ${{ vars.KUPE_API_URL }}
          KUPE_API_TOKEN: ${{ secrets.KUPE_API_TOKEN }}
          KUPE_TEST_TENANT: ${{ vars.KUPE_TEST_TENANT }}
```

See [distribution.md](./distribution.md) for release workflow; this doc covers only CI.

## Coverage targets

Not blocking, informational:

- Layer 1 (commands): 80%
- Layer 2 (printer): focused coverage for parser, table, detail, and ANSI alignment behavior
- Layer 3 (client): 90%
- Layer 4 (live): cover happy paths for every command that can run safely against the shared testing tenant

Coverage is available through `make test-coverage`; it is not currently a hard CI gate.
