# IMPLEMENTATION.md — v1 delivery plan

This document breaks the kupe-cli v1 delivery into numbered phases. Each phase is a
shippable increment: the binary compiles, tests pass, and a user can do one more
thing at the end of it than they could at the start. Ship phases as individual PRs;
do not wait for the whole set.

Read [docs/architecture.md](./docs/architecture.md) and [docs/design.md](./docs/design.md)
first — the phases here implement the shapes decided there.

**Status legend:** 🟢 done · 🟡 in progress · ⚪ pending

---

## Phase 0 — Scaffolding 🟢

Goal: compilable binary with `kupe version`, `kupe completion`, and the full
global-flag surface registered on root. CI green on a PR with no Go changes.

- Module: `github.com/kupecloud/kupe-cli`, Go 1.26.2.
- Files: [cmd/kupe/main.go](./cmd/kupe/main.go), [internal/build/info.go](./internal/build/info.go),
  [internal/cmd/root.go](./internal/cmd/root.go), [internal/cmd/version.go](./internal/cmd/version.go),
  [internal/cmd/completion.go](./internal/cmd/completion.go).
- Repo files: [Makefile](./Makefile), [.golangci.yaml](./.golangci.yaml), [.goreleaser.yaml](./.goreleaser.yaml),
  [.pre-commit-config.yaml](./.pre-commit-config.yaml), [.github/workflows/](./.github/workflows/),
  [.gitignore](./.gitignore), [.codespellignore](./.codespellignore).
- Dependencies: `github.com/spf13/cobra`.

**Acceptance criteria** — all met:

- [x] `make build` produces `bin/kupe`.
- [x] `./bin/kupe version` prints `kupe version dev (commit ..., built ...)`.
- [x] `./bin/kupe version -o json` prints JSON.
- [x] `./bin/kupe completion zsh` outputs a completion script.
- [x] `./bin/kupe --help` shows global flags.
- [x] `pre-commit run` passes on all tracked files.
- [x] `make lint` runs cleanly (gofmt/vet/golangci-lint).

---

## Phase 1 — Config + auth foundation 🟢

Goal: a user can `kupe auth login --tenant X --token kupe_...` and `kupe config use-context NAME`.
No API calls yet (validation happens in Phase 2 when the client lands), but the full token
lifecycle works offline.

### Packages to create

- `internal/config/` — YAML config loader.
  - `schema.go` — `Config`, `Context`, `Preferences` structs matching the schema in
    [docs/auth.md](./docs/auth.md).
  - `config.go` — load, save (atomic write via tempfile + rename), default path
    resolution (XDG on Linux, `%AppData%` on Windows).
  - `precedence.go` — single `Resolve(flags, env, file)` function returning the
    final `token`, `apiUrl`, `tenant`, `context` per the precedence chain in
    [docs/auth.md](./docs/auth.md).
- `internal/auth/` — token storage.
  - `keyring.go` — thin wrapper around `github.com/zalando/go-keyring` with
    service key `kupe-cli` and per-context account key.
  - `plaintext.go` — fallback to `~/.config/kupe/credentials.yaml` (mode 0600)
    when keyring unavailable or when `KUPE_STORAGE=plaintext`.
  - `token.go` — high-level `Get(context) (string, error)`, `Set(context, token)`,
    `Delete(context)` that picks keyring vs plaintext.
- `internal/cli/` — factory + IOStreams.
  - `iostreams.go` — stdin/stdout/stderr wrapper with `ColorEnabled`,
    `SpinnersEnabled`, `PromptsEnabled` per [docs/output.md](./docs/output.md).
  - `factory.go` — `Factory` struct with lazy `Config()` and (later) `Client()`
    functions, injected into every subcommand.
  - `exit.go` — `ExitCodeFrom(err error) int` mapping typed client errors (once
    they exist) to exit codes per [docs/design.md](./docs/design.md).
- `internal/cmd/auth/` — login, logout.
  - `auth.go` — parent command.
  - `login.go` — `--tenant`, `--token`, `--api-url`, `--context`, `--set-default`.
    TTY path prompts for token with `term.ReadPassword`; non-TTY requires `--token`.
  - `logout.go` — `--context` or `--all`. Deletes keyring entry, marks context
    `tokenRef: ""` in config.
- `internal/cmd/config/` — view, get, set, use-context, set-context, delete-context,
  current-context (all specified in [docs/commands.md](./docs/commands.md)).

### Dependencies to add

- `gopkg.in/yaml.v3`
- `github.com/zalando/go-keyring`
- `golang.org/x/term`

### Tests

- `internal/config/precedence_test.go` — table-driven, covers every layer of the
  chain and error cases.
- `internal/config/config_test.go` — round-trip load/save; atomic write semantics
  (partial write doesn't corrupt existing file); token redaction on `View`.
- `internal/auth/token_test.go` — keyring backend via a mock keyring interface;
  plaintext fallback path; error when keyring is hard-required via `KUPE_STORAGE=keyring`.
- `internal/cmd/auth/login_test.go` — factory-injected login with both TTY and
  non-TTY paths, exit-3 on missing token in non-TTY.
- `internal/cmd/config/*_test.go` — each subcommand, happy-path + invalid-key error.

### Acceptance criteria — all met

- [x] `kupe auth login --tenant foo --token kupe_...` creates `~/.config/kupe/config.yaml`
      and stores the token in the keyring (or credentials.yaml fallback).
- [x] `kupe config view` shows the context with `tokenRef: keyring|plaintext` (no token leaked).
- [x] `kupe config use-context NAME` flips `currentContext`; exit 4 if the context is missing.
- [x] `kupe auth logout` removes the token; `view` shows no `tokenRef`.
- [x] `KUPE_API_TOKEN=... kupe auth login` refuses with a helpful message (exit 3).
- [x] Non-interactive login without `--tenant` / `--token` exits 2 with a clear message.
- [x] `kupe config delete-context` prompts on TTY, requires `--yes` in CI.
- [x] All pre-commit hooks pass; `make test` green (config, auth, cli, cmd/auth).

---

## Phase 2 — HTTP client 🟢

Goal: ground-truth API access. `kupe auth whoami` returns real data from `kupe-api`.
Nothing else exposed yet, but the client is ready for cluster/apikey commands.

### Packages to create

- `internal/client/` — lifted from [terraform-provider-kupe/internal/client/](../terraform-provider-kupe/internal/client/)
  and extended per [docs/api-client.md](./docs/api-client.md).
  - `client.go` — base HTTP plumbing (copy from tf provider, swap User-Agent).
  - `errors.go` — extend `APIError` with `RequestID`; add `IsUnauthorized`,
    `IsForbidden`, `IsValidation`, `IsPreconditionFailed`, `IsRateLimited`,
    `IsUnavailable` helpers.
  - `retry.go` — exponential backoff on 502/503/504 + network errors.
    Non-retrying for POST/PATCH/PUT.
  - `ratelimit.go` — `Retry-After` parsing (seconds + HTTP-date), one retry only.
  - `tenant.go` — `GetTenant(ctx)` for `whoami`.
  - `interface.go` — `Interface` interface containing every method commands use
    (lets tests inject a fake).

- `internal/client/clienttest/` — in-memory fake implementing `client.Interface`.

- `internal/cmd/auth/whoami.go` — wire through the factory's `Client()`.

### Dependencies to add

None new; stdlib `net/http`.

### Tests

- `internal/client/client_test.go` — httptest fixtures covering happy path, every
  error class (401/403/404/400/409/412/429/503), network error retry, 429 retry-after.
- `internal/client/retry_test.go` — table-driven retry policy, including "POST never
  retries" assertion.
- `internal/client/ratelimit_test.go` — Retry-After parsing variants.
- `internal/cmd/auth/whoami_test.go` — factory with fake client, `-o json` output.

### Acceptance criteria — all met

- [x] `kupe auth whoami` hits `GET /api/v1/tenants/{tenant}` and renders a table.
- [x] `kupe auth whoami -o json` emits the documented JSON schema.
- [x] 401 surfaces as `Error: ... (request-id: ...)` on stderr and exit 3.
- [x] 503 on `kupe-api` retries transparently and still succeeds when the server recovers.
- [x] `kupe auth login` validates against the API before persisting; 401/403/404 → no config
      written, no token in keyring.
- [x] POST/PATCH/PUT never retry (verified by test), protecting against duplicate writes.
- [x] Context cancellation (Ctrl+C) short-circuits retry loops in < 500ms.

---

## Phase 3 — Cluster CRUD + polling 🟢

Goal: a user can go from zero to a cluster in Running state, and back to gone,
using only the CLI. This is the headline phase for the "60-second quickstart."

### Packages to create or extend

- `internal/client/cluster.go` — lift from tf provider: `Cluster`, `CreateClusterRequest`,
  `PatchClusterRequest`, `ListClusters`, `GetCluster`, `CreateCluster`,
  `UpdateCluster`, `DeleteCluster`.
- `internal/client/cluster_rmw.go` — `UpdateClusterRMW(ctx, name, mutator)` per
  [docs/api-client.md](./docs/api-client.md).
- `internal/printer/` — output rendering.
  - `printflags.go` — thin wrapper around `k8s.io/cli-runtime/pkg/genericclioptions.PrintFlags`.
  - `table.go` — table-printer primitives (column spec, tabwriter rendering, color gate).
  - `cluster_table.go` — column specs for Cluster (NAME/TYPE/VERSION/PHASE/CPU/MEM/AGE)
    per [docs/output.md](./docs/output.md).
  - `template.go`, `jsonpath.go` — delegate to cli-runtime (thin).
- `internal/ux/` — long-op rendering.
  - `style.go` — lipgloss palette (Running=green, Provisioning=yellow, Degraded=red).
  - `plain.go` — non-TTY "[00:00:04] phase" stderr ticks.
  - `spinner.go` — Bubbletea standalone program for single-line TTY status.
  - `progress.go` — polling loop driver: picks spinner vs plain based on
    `iostreams.SpinnersEnabled`.
- `internal/cmd/cluster/` — the full verb set.
  - `cluster.go` — parent.
  - `list.go`, `get.go`, `create.go`, `delete.go`, `update.go`, `wait.go`.

### Dependencies to add

- `k8s.io/cli-runtime` — printers, PrintFlags.
- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`

### Tests

- `internal/printer/*_test.go` — golden files in `testdata/golden/` for every
  `-o` × every resource.
- `internal/ux/progress_test.go` — fake client that walks a phase sequence;
  both TTY-forced and CI-forced streams produce expected output (text snapshot).
- `internal/cmd/cluster/*_test.go` — factory tests for each command; `--wait=false`
  short-circuit; prompt suppression in non-TTY.
- `test/e2e/cassettes/cluster_create_wait.yaml` — go-vcr cassette for a real
  create→wait→delete cycle.

### Acceptance criteria — all met

- [x] `kupe cluster create prod --type shared --version 1.32` waits to Running
      with a Bubbletea single-line TTY spinner (inline, stderr).
- [x] In CI mode (`CI=true` or non-TTY stderr) plain stderr ticks print per
      phase transition, no ANSI escapes.
- [x] `kupe cluster list` renders a kubectl-style table; `-o json`/`-o yaml`/
      `-o name`/`-o go-template=...` all work.
- [x] `kupe cluster update prod --version 1.33` uses ETag RMW; 412 retried
      once, then `ErrRMWContention` → exit 5.
- [x] `kupe cluster delete prod` prompts on TTY, requires `--yes` in CI
      (exit 2), waits for 404 by default.
- [x] `kupe cluster wait prod --for running --timeout 5m` → exit 8 on timeout
      via `cli.TimeoutError`.
- [x] Context cancellation surfaces through to the `ux.WaitFor` path (verified
      via tests).

---

## Phase 4 — Kubeconfig + exec plugin 🟢

Goal: `kupe cluster kubeconfig prod --merge` produces a working kubectl context
in under two seconds. `--exec` mode works end-to-end with `kubectl`.

### Packages to create

- `internal/kubeconfig/`
  - `build.go` — assemble a kubeconfig YAML from `{endpoint, certificateAuthority}`
    plus either an embedded token or an exec-plugin stanza. No hand-rolled YAML —
    use `k8s.io/client-go/tools/clientcmd/api/v1`.
  - `merge.go` — atomic merge into `$KUBECONFIG` / `~/.kube/config` via
    `clientcmd.Merge`; collision handling with `--force` vs error.
- `internal/cmd/cluster/kubeconfig.go` — flags `--merge`, `--context-name`,
  `--user-name`, `--cluster-name`, `--exec`, `--minify`, `--force`.
- `internal/cmd/auth/get_token.go` — emits `client.authentication.k8s.io/v1`
  `ExecCredential` per [docs/auth.md](./docs/auth.md). Not user-facing; called by
  kubectl.

### Dependencies to add

- `k8s.io/client-go/tools/clientcmd`
- `k8s.io/client-go/pkg/apis/clientauthentication/v1`

### Tests

- `internal/kubeconfig/build_test.go` — token mode vs exec mode renders; golden
  YAML files.
- `internal/kubeconfig/merge_test.go` — fresh file, merge-into-existing, collision
  without `--force`, collision with `--force`, atomic-write preserves original
  on error.
- `internal/cmd/auth/get_token_test.go` — ExecCredential JSON schema round-trip.
- `test/e2e/` — cassette `cluster_kubeconfig_merge.yaml` end-to-end.

### Acceptance criteria — all met

- [x] `kupe cluster kubeconfig prod` prints a valid kubectl kubeconfig with
      endpoint, CA, and embedded bearer token.
- [x] `kupe cluster kubeconfig prod --merge` writes into `$KUBECONFIG` (or
      `~/.kube/config`), preserves other contexts, detects collisions, is
      idempotent for identical input, writes with mode 0600.
- [x] `--exec` emits an exec-plugin kubeconfig that carries no bearer token;
      the exec stanza shells back to `kupe auth get-token --context=...`
      using an absolute binary path.
- [x] `kupe auth get-token` (hidden subcommand) emits a valid
      `client.authentication.k8s.io/v1` `ExecCredential` JSON with the
      resolved token; exits 3 when no credentials are available.
- [x] 503 from kupe-api (cluster not yet Running) maps to exit 7 with a hint
      pointing at `kupe cluster wait`.
- [x] 404 maps to exit 4; collisions without `--force` map to exit 5.

---

## Phase 5 — API key commands 🟢

Goal: devs can mint and revoke tokens from the CLI. Closes the self-service loop
for CI pipelines.

### Packages to create or extend

- `internal/client/apikey.go` — lift from tf provider. Fields per the response
  shape in [kupe-api/internal/server/handler_apikey.go](../kupe-api/internal/server/handler_apikey.go).
- `internal/printer/apikey_table.go` — ID/NAME/ROLE/CREATED BY/LAST USED/AGE/EXPIRES.
- `internal/cmd/apikey/` — list, create, delete.

### Tests

- `internal/client/apikey_test.go` — httptest fixtures.
- `internal/cmd/apikey/*_test.go` — factory tests, including the create-once
  secret printing behavior (raw token to stdout, metadata to stderr on TTY).
- Golden files for list/create output.

### Acceptance criteria — all met

- [x] `kupe apikey create --name ci --role admin` prints the raw token once
      to stdout; metadata to stderr only when stderr is a TTY. Non-TTY stdout
      is pipeable as `TOKEN=$(...)`.
- [x] `-o json` emits a stable v1 schema `{id, name, role, token, createdAt, expiresAt}`.
      Raw token never leaks via `apikey list` (server-side nor fake-side).
- [x] `kupe apikey list` table truncates IDs; `-o wide` shows full IDs +
      EXPIRES.
- [x] `kupe apikey delete ID` prompts on TTY, requires `--yes` in CI (exit 2);
      404 → exit 4.
- [x] 403 from a readonly caller → exit 3 via the generic `*APIError` mapping.
- [x] `--expires-at` accepts `7d`, `24h`, and RFC3339; invalid input → exit 2.
- [x] POST /apikeys never retries on 503 (duplicate-creation safety).

---

## Phase 6 — Distribution + first release 🟢 (repo-side) / ⚪ (release cut)

Goal: the binary is installable via `brew tap kupecloud/tap && brew install kupe`
and `curl -fsSL https://get.kupe.cloud | sh`. A v0.1.0 tag exists on GitHub
with signed checksums and SBOMs.

### Artifacts — in-repo work 🟢

- [x] [LICENSE](./LICENSE) — Apache 2.0 (mirrors terraform-provider-kupe).
- [x] [scripts/install.sh](./scripts/install.sh) — POSIX install script,
      tested end-to-end on macOS. Detects OS/arch, resolves latest version
      from the GitHub API (or `--version`), downloads + SHA-256-verifies,
      installs to `/usr/local/bin` or `~/.local/bin` (`--user`), prints a
      PATH hint if the install dir isn't on `$PATH`.
- [x] [.goreleaser.yaml](./.goreleaser.yaml) — renamed `project_name` to
      `kupe` so the generated Homebrew cask is `kupe.rb` with `binary "kupe"`
      (was `kupe-cli`, which would have forced `brew install
      kupecloud/tap/kupe-cli`). Scoop manifest is `kupe.json`. `make snapshot`
      verified to produce 6 archives (darwin/linux/windows × amd64/arm64) +
      checksums + both manifests cleanly.
- [x] [.github/workflows/release.yaml](./.github/workflows/release.yaml) —
      already scaffolded in Phase 0; installs Cosign + Syft + GoReleaser on
      the runner, uses `id-token: write` for keyless signing.
- [x] [RELEASING.md](./RELEASING.md) — runbook covering one-time setup (tap
      repo, Scoop bucket repo, PAT creation, install-script hosting) and the
      per-release flow (tag → CI → verify `brew install` + `curl | sh`).
- [x] README updated with `brew tap kupecloud/tap; brew install kupe` as the
      primary path (plus the auto-tap one-liner as a fallback note).

### Out-of-repo setup required before first release

Tracked in [RELEASING.md](./RELEASING.md) — each of these is a human action:

- [ ] Create empty GitHub repo `kupecloud/homebrew-tap`.
- [ ] Create empty GitHub repo `kupecloud/scoop-bucket`.
- [ ] Mint fine-grained PATs (contents:write scope, scoped to each tap
      repo), add as `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` secrets
      on `kupecloud/kupe-cli`.
- [ ] Host `scripts/install.sh` at `https://get.kupe.cloud` via Cloudflare
      Pages or Worker.

### Acceptance criteria — to verify once above setup lands

- [ ] `git tag v0.1.0 && git push origin v0.1.0` triggers the release
      workflow.
- [ ] GitHub release contains 6 archives + `checksums.txt` + Cosign
      signature + per-archive SBOM.
- [ ] `brew tap kupecloud/tap && brew install kupe` yields a working
      `kupe version` showing `v0.1.0`.
- [ ] `scoop bucket add kupe https://github.com/kupecloud/scoop-bucket &&
      scoop install kupe` works on Windows.
- [ ] `curl -fsSL https://get.kupe.cloud | sh` installs into
      `/usr/local/bin/kupe`.

---

## Phase 1.5 — OIDC device flow 🟢 (shipped)

`kupe auth login --method oidc` runs the RFC 8628 device-code flow against
Authentik's `kupe-cli` public client. Refresh tokens rotate transparently;
the `--exec` kubeconfig path triggers an in-process refresh on each kubectl
call when the access token is near expiry.

- [x] `internal/auth/oidc.go` — discovery, refresh, id_token email extraction.
- [x] `internal/auth/oidc_device.go` — RFC 8628 device flow via
      `golang.org/x/oauth2.DeviceAuth` + `DeviceAccessToken`.
- [x] `internal/cmd/auth/login.go` — `--method oidc` (default), prompt prints
      user code + verification URL, best-effort opens browser.
- [x] `internal/cmd/auth/get_token.go` — refreshes when within `refreshSkew`
      of expiry; clears stored token + asks for re-login on `invalid_grant`.
- [x] Authentik blueprint — `kupe-cli` provider as public client; brand
      `flow_device_code` set; `access_code_validity: 10m`.

---

## Phase 7 — Secrets + members 🟢

Goal: cover the rest of tenant-scoped resources.

- [x] `internal/client/secret.go` + `member.go` — lifted from tf-provider,
      plus `ListSecrets` (not present on the tf side) and
      `UpdateSecretRMW` with one 412-retry → `ErrSecretRMWContention`.
- [x] `internal/printer/secret.go` + `member.go` — column specs; Secret
      list truncates path to `wide`, shows sync count + deduped cluster
      list; Member is a simple EMAIL/ROLE table.
- [x] `internal/cmd/secret/` — `list`, `get`, `create`, `update`, `delete`.
      `--sync cluster:namespace[:secretName]` is repeatable; `update`
      replaces the full sync list via RMW.
- [x] `internal/cmd/member/` — `list`, `add`, `update` (role-only), `remove`.
      Role defaults to `readonly` on add; invalid role → exit 2.
- [x] `client.Interface` + `clienttest.Fake` extended.
- [x] Registered in root; `kupe --help` now shows all 8 noun commands.

### Acceptance criteria — all met

- [x] `kupe secret list` table shows NAME/PHASE/SYNCS/AGE; `-o wide`
      adds PATH + dedupe'd CLUSTERS column.
- [x] `kupe secret create NAME --path PATH` persists; `--sync A:B` and
      `--sync A:B:C` both parse; invalid `--sync` → exit 2.
- [x] `kupe secret update NAME --sync ...` uses the RMW helper; 412
      retried once internally; persistent 412 → exit 5.
- [x] `kupe secret delete NAME` prompts on TTY; requires `--yes` in CI.
- [x] `kupe member add EMAIL` defaults to readonly; `--role` accepts
      only admin/readonly.
- [x] `kupe member add` duplicate → 409 → exit 5.
- [x] `kupe member remove EMAIL` prompts on TTY; requires `--yes` in CI;
      404 → exit 4.

### Tests

- `client` — ListSecrets, CreateSecret conflict, UpdateSecretRMW happy
  path + 412-retry, DeleteSecret 404. ListMembers, AddMember happy +
  conflict, UpdateMember role-only, RemoveMember + 404.
- `cmd/secret` — list empty, create + list round-trip, `--path` required,
  `--sync` parsing (2-field and 3-field forms; invalid → exit 2), update
  replaces sync list, update without `--sync` → exit 2, delete non-TTY
  refusal, delete yes, get not-found → exit 4, `parseSyncTargets` table.
- `cmd/member` — list, add default role, add conflict → exit 5, invalid
  role → exit 2, update role, remove non-TTY refusal, remove yes, remove
  not-found → exit 4, list -o json.

---

## Phase 8 — TUI ⚪

Goal: `kupe tui` launches a k9s-style interactive view.

See [docs/tui.md](./docs/tui.md) for the full design. Implement in its own PR per
page (list → detail → confirm → command mode → filter).

---

## Cross-cutting standards

### Every PR must

- Pass `make lint`, `make test`, `pre-commit run --all-files`.
- Include tests for new code (see layer mapping in [docs/testing.md](./docs/testing.md)).
- Update [docs/commands.md](./docs/commands.md) if adding a user-visible command
  or flag.
- Follow conventional-commits single-line messages (enforced by
  `conventional-pre-commit`).

### Dependency hygiene

- Keep `go mod vendor` up-to-date — PR diffs should show `vendor/` changes
  alongside `go.mod` / `go.sum`.
- Pin major versions in `go.mod`; use `go get -u` judiciously.
- Run `make govulncheck` before cutting any release.

### Error handling

- Every HTTP error path returns a typed error (`*client.APIError` or a wrapping
  of one). Command-layer code uses `Is*` helpers, never string matching.
- Exit codes are set by `internal/cli/exit.go` based on the error type — not by
  individual commands.

### Logging

- `-v` flag enables debug logs to stderr via `slog`.
- Never log `Authorization` headers, token values, or full request/response
  bodies.
- Use structured key-value pairs: `slog.Debug("request", "method", m, "path", p, "status", s)`,
  not `fmt.Sprintf`.

---

## Unresolved decisions (flag before implementation)

These are tracked but not blocking scaffolding:

1. **Keyring required vs with-fallback** — current plan has plaintext fallback
   for headless Linux; reconsider if the keyring code path is flaky in CI testing.
2. **Telemetry** — currently off in v1. Revisit post-v1 if install/usage stats
   become important.
3. **Shared `kupe-go-client` module extraction** — when does the tf provider's
   lifted client graduate to a standalone module? Target: early Phase 7, once
   the surface has stabilized.
4. **Windows Authenticode signing** — deferred. Cosign keyless on checksums is
   the v1 story. Windows Defender may warn on first run; document it.
5. **`--pager` flag** — deferred. Users pipe to `less` themselves.
