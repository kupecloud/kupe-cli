---
title: "Testing"
description: "Factory + IOStreams pattern, golden-file tests, fake client, httptest fixtures, go-vcr cassettes, and E2E strategy for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 8
---

The CLI's test strategy has four layers, each owning a specific failure class. All layers are run by `make test` in CI; the live E2E layer is gated on an env var so it doesn't fire on PRs from forks.

| Layer | Scope | Purpose | Runs in CI? |
|-------|-------|---------|-------------|
| 1. Command unit tests | `internal/cmd/...` | Flag parsing, rendering, error mapping, exit codes | Yes |
| 2. Golden-file tests | `internal/printer/...` | Output format stability per resource | Yes |
| 3. Client tests | `internal/client/...` | HTTP plumbing, retries, ETag, error classification | Yes |
| 4. E2E tests | `test/e2e/...` | Full round-trip against a real `kupe-api` | On schedule / on-demand |

The single goal: **changes to a command body fail a test; changes to an output schema fail a golden file; changes to API contract fail an E2E or a VCR cassette.** Each break has one obvious place to look.

## Layer 1 — command unit tests

### Pattern: Factory + IOStreams

Every command body accepts a `*cli.Factory`. Tests construct a factory backed by in-memory IOStreams and fakes:

```go
func TestClusterList_JSON(t *testing.T) {
    f, out, _ := cmdtest.NewTestFactory(t,
        cmdtest.WithClusters([]client.Cluster{
            {Name: "prod", Type: "shared", Status: &client.ClusterStatus{Phase: "Running"}},
        }),
    )
    cmd := clustercmd.NewListCmd(f)
    cmd.SetArgs([]string{"-o", "json"})

    err := cmd.Execute()
    require.NoError(t, err)

    var got []client.Cluster
    require.NoError(t, json.Unmarshal(out.Bytes(), &got))
    require.Len(t, got, 1)
    require.Equal(t, "prod", got[0].Name)
}
```

Helpers live in `internal/cmdtest/`:

- `NewTestFactory(t, opts...)` — returns `(*cli.Factory, *bytes.Buffer, *bytes.Buffer)` for stdout and stderr.
- `WithClusters(...)`, `WithAPIKeys(...)`, `WithTenant(...)` — preload a fake client.
- `WithConfig(*config.Config)` — override resolved config.
- `WithTTY(stdinTTY, stdoutTTY, stderrTTY bool)` — control the detected TTY state to exercise TTY/CI code paths.

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

## Layer 2 — golden-file tests

Every output format × every resource gets a golden file. Update with `go test -update`.

```
test/golden/
├── cluster_list_table.txt
├── cluster_list_wide.txt
├── cluster_list_json.json
├── cluster_list_yaml.yaml
├── cluster_list_name.txt
├── cluster_get_table.txt
├── cluster_get_yaml.yaml
├── apikey_list_table.txt
├── apikey_create_stdout.txt
├── ...
```

Pattern (adapted from `kubectl` and `helm`):

```go
func TestClusterListGolden(t *testing.T) {
    fixture := loadFixture(t, "clusters-basic.json")
    for _, fmt := range []string{"table", "wide", "json", "yaml", "name"} {
        t.Run(fmt, func(t *testing.T) {
            f, out, _ := cmdtest.NewTestFactory(t,
                cmdtest.WithClustersRaw(fixture),
                cmdtest.WithTTY(true, true, true),
            )
            cmd := clustercmd.NewListCmd(f)
            cmd.SetArgs([]string{"-o", fmt})
            require.NoError(t, cmd.Execute())

            goldenPath := filepath.Join("testdata/golden",
                fmt.Sprintf("cluster_list_%s.%s", fmt, ext(fmt)))
            cmdtest.AssertGolden(t, out.Bytes(), goldenPath)
        })
    }
}
```

`cmdtest.AssertGolden` compares to the on-disk file; with `-update`, it writes the current output back. Humans review golden-file changes like any other diff.

### Determinism

- Fixtures use **fixed timestamps** (`2026-04-20T14:00:00Z`) so `AGE` columns are stable.
- `TestMain` sets `NO_COLOR=1` and forces the detected TTY state explicitly per test — no environment leakage.
- `time.Now()` is injected via a clock interface on the factory so "elapsed time" rendering is stable.

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

    c := client.New(srv.URL, "acme", "test-token")
    _, _, err := c.GetCluster(context.Background(), "missing")
    require.True(t, client.IsNotFound(err))
    require.Contains(t, err.Error(), "test-req-123")
}
```

Coverage targets:

- All resource CRUD (list/get/create/update/delete) for cluster + apikey.
- Every typed error helper (`IsUnauthorized`, `IsForbidden`, `IsNotFound`, `IsValidation`, `IsConflict`, `IsPreconditionFailed`, `IsRateLimited`, `IsUnavailable`).
- Retry policy: transient 503 retries, persistent 503 fails after N attempts, POST/PATCH never retry.
- `Retry-After` parsing: integer, HTTP-date, missing, malformed.
- ETag RMW: happy path, 412 retry, 412 twice fails.
- Request-ID propagation into errors.

### Imported tests from terraform-provider-kupe

When lifting `internal/client/*.go`, the accompanying `*_test.go` files come too. They already cover most of the HTTP contract. Add new tests only for the extensions (retry, `Retry-After`, typed helpers, RMW).

## Layer 4 — E2E

Two modes, triggered by separate env vars.

### Mode A — go-vcr cassettes (deterministic)

[`gopkg.in/dnaeon/go-vcr.v3`](https://pkg.go.dev/gopkg.in/dnaeon/go-vcr.v3) records real `kupe-api` responses the first time, replays them on subsequent runs. Cassettes are committed to `test/e2e/cassettes/`.

```go
func TestE2E_ClusterCreateWait(t *testing.T) {
    r, err := recorder.New("testdata/cassettes/cluster_create_wait",
        recorder.WithMode(recorder.ModeReplayOnly),
    )
    require.NoError(t, err)
    defer r.Stop()

    httpClient := r.GetDefaultClient()
    c := client.NewWithHTTP(os.Getenv("KUPE_E2E_URL"), "acme", "test-token", httpClient)
    // exercise create + wait
}
```

Runs always in CI. Any PR that changes HTTP plumbing may need cassettes re-recorded, which is itself a review signal.

### Mode B — live `kupe-api` (`make test-live`, in repo)

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

**Auth-mode parity (OIDC vs apikey):** the same suite runs unmodified for either token type — the CLI's `--token` / `KUPE_API_TOKEN` path doesn't care if the bearer is a `kupe_…` API key or an OIDC JWT. To verify both auth paths work end-to-end, run `make test-live` twice: once with an apikey and once with a JWT minted via `kupe auth login --method oidc` (read it out of the keyring with `security find-generic-password -s cloud.kupe.cli -a kupe-test -w` on macOS).

**Manual OIDC smoke test:** the auth-code+PKCE login itself can't be automated in `go test` — it needs a real browser and real Authentik. Test it interactively against dev:

```bash
unset KUPE_API_TOKEN
./bin/kupe auth login \
    --tenant kupe-test \
    --api-url https://api.dev.int.kupe.cloud \
    --oidc-issuer https://auth.dev.int.kupe.cloud/application/o/kupe-cli/
./bin/kupe auth whoami        # should report your email + the tenant
./bin/kupe cluster list       # any authenticated noun proves the JWT path
```

**Not wired into GitHub Actions** — intentional. Live tests need WireGuard for the private dev API, mutate state in the testing tenant, and would slow PR feedback. Promote to nightly only if/when the operations matter.

### Mode C — aspirational live e2e (not yet built)

Gated by `KUPE_E2E_URL` + `KUPE_E2E_TOKEN`. Runs against a dev instance of `kupe-api`:

```bash
export KUPE_E2E_URL=https://api.dev.kupe.cloud
export KUPE_E2E_TOKEN=kupe_dev_...
export KUPE_E2E_TENANT=cli-e2e

make test-e2e-live
```

Test plan:

1. Create a cluster with `--wait` and a short timeout.
2. `kupe cluster get` returns it.
3. `kupe cluster kubeconfig --merge` into a tempfile, then `kubectl --kubeconfig=$tmp get namespaces` succeeds.
4. Update to a new minor version, wait for `Running`.
5. Delete, wait for gone.
6. Create + revoke an API key.

Runs on a schedule (nightly) and on-demand via `/test live-e2e` on a PR. Not on every PR.

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

When adding a new command (e.g., `kupe secret create` in Phase 2), the test checklist is:

- [ ] Unit test: happy path, `-o json`.
- [ ] Unit test: not-found error on a dependent resource.
- [ ] Unit test: validation error on missing required flag.
- [ ] Unit test: `--yes` skips prompt; non-TTY without `--yes` fails.
- [ ] Golden files for `table`, `wide` (if applicable), `json`, `yaml`, `name`.
- [ ] Client test: happy path.
- [ ] Client test: error per classification the API can emit.
- [ ] Commands.md entry.

A one-page "adding a command" guide lives in this repo's `CONTRIBUTING.md` (Phase 2 deliverable; not part of this doc-writing task).

## Lint and vuln

Mirror [kupe-api](../../kupe-api/) and [terraform-provider-kupe](../../terraform-provider-kupe/):

```bash
make lint    # golangci-lint run
make sec     # gosec + govulncheck
make vet     # go vet
```

Golangci-lint config sits next to the existing kupe repos' `.golangci.yml`. Same enabled linters, same `errcheck` exclusions (context-scoped, deferred `Close()`), same import-ordering rules. Copy rather than inventing a new one.

`govulncheck` runs on every PR; high-severity advisories block the merge. Low-severity advisories warn only.

## CI workflow summary

`.github/workflows/ci.yaml`:

```yaml
jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.26.2" }
      - run: make vendor
      - run: make lint
      - run: make test                   # layers 1 + 2 + 3
      - run: make test-e2e-cassette      # layer 4 mode A
      - run: make sec
      - run: goreleaser release --snapshot --clean --skip=publish
```

`.github/workflows/e2e-live.yaml` (schedule + `workflow_dispatch`):

```yaml
on:
  schedule: [{cron: "0 4 * * *"}]
  workflow_dispatch: {}
jobs:
  live:
    environment: e2e
    steps:
      - run: make test-e2e-live
        env:
          KUPE_E2E_URL: ${{ vars.KUPE_E2E_URL }}
          KUPE_E2E_TOKEN: ${{ secrets.KUPE_E2E_TOKEN }}
          KUPE_E2E_TENANT: ${{ vars.KUPE_E2E_TENANT }}
```

See [distribution.md](./distribution.md) for release workflow; this doc covers only CI.

## Coverage targets

Not blocking, informational:

- Layer 1 (commands): 80%
- Layer 2 (printer): 100% (every format × every resource)
- Layer 3 (client): 90%
- Layer 4 (E2E cassettes): cover the happy path for every command in v1

Coverage reported via `go test -coverprofile` and posted as a PR comment. A regression of >2% blocks merge; smaller changes just annotate.
