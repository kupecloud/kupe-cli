---
title: "Command Reference"
description: "Synopsis, flags, and examples for every kupe CLI subcommand in the v1 scope"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 3
---

This is the v1 command reference. Every command follows the grammar in [design.md](./design.md): `kupe <noun> <verb> [NAME] [--flags]`. Every command inherits the auth/config global flags described there. Output flags are local to commands because supported formats differ by command.

Commands in this document map one-to-one to endpoints in [kupe-api/api/swagger.json](../../kupe-api/api/swagger.json). Any endpoint not listed here is either in Phase 2 or intentionally not exposed via CLI.

## Coverage vs. kupe-api endpoints

| Endpoint | CLI command | Status |
|----------|-------------|--------|
| `GET /api/v1/tenants/{tenant}` | `kupe tenant get` (also `kupe auth whoami`) | v1 |
| `PATCH /api/v1/tenants/{tenant}` | — | deferred |
| `GET /api/v1/tenants/{tenant}/members` | `kupe member list` | v1 |
| `POST /api/v1/tenants/{tenant}/members` | `kupe member add` | v1 |
| `PATCH /api/v1/tenants/{tenant}/members/{email}` | `kupe member update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/members/{email}` | `kupe member remove` | v1 |
| `GET /api/v1/tenants/{tenant}/clusters` | `kupe cluster list` | v1 |
| `POST /api/v1/tenants/{tenant}/clusters` | `kupe cluster create` | v1 |
| `GET /api/v1/tenants/{tenant}/clusters/{name}` | `kupe cluster get` | v1 |
| `PATCH /api/v1/tenants/{tenant}/clusters/{name}` | `kupe cluster update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/clusters/{name}` | `kupe cluster delete` | v1 |
| `GET /api/v1/tenants/{tenant}/clusters/{name}/kubeconfig` | `kupe cluster kubeconfig` | v1 |
| `GET /api/v1/tenants/{tenant}/apikeys` | `kupe apikey list` | v1 |
| `POST /api/v1/tenants/{tenant}/apikeys` | `kupe apikey create` | v1 |
| `DELETE /api/v1/tenants/{tenant}/apikeys/{id}` | `kupe apikey delete` | v1 |
| `GET /api/v1/tenants/{tenant}/secrets` | `kupe secret list` | v1 |
| `POST /api/v1/tenants/{tenant}/secrets` | `kupe secret create` | v1 |
| `GET /api/v1/tenants/{tenant}/secrets/{name}` | `kupe secret get` | v1 |
| `PATCH /api/v1/tenants/{tenant}/secrets/{name}` | `kupe secret update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/secrets/{name}` | `kupe secret delete` | v1 |
| `GET /api/v1/tenants/{tenant}/invoices` | `kupe invoice list` | v1 |
| `GET /api/v1/tenants/{tenant}/invoices/{name}` | `kupe invoice get` | v1 |
| `GET /api/v1/plans` | `kupe plan list` | v1 |
| `GET /api/v1/plans/{name}` | `kupe plan get` | v1 |
| `GET /api/v1/tenants/{tenant}/alertmanager/config` | — | planned (see below) |
| `GET,PUT,DELETE /api/v1/tenants/{tenant}/alertmanager/receivers[/{name}]` | — | planned (see below) |
| `GET,PUT,DELETE /api/v1/tenants/{tenant}/alertmanager/routes[/{id}]` | — | planned (see below) |
| `GET,PUT /api/v1/tenants/{tenant}/alertmanager/global` | — | planned (see below) |

---

## `kupe version`

Print version, git commit, build date, Go runtime, and platform. Useful for support tickets.

```
$ kupe version
kupe version 0.1.0 (commit 7a3b9e4, built 2026-04-20T14:02:11Z, go1.26.3 darwin/arm64)
```

Use `kupe version --short` for just the version string.

With `-o json`:

```json
{
  "version": "0.1.0",
  "commit": "7a3b9e4",
  "buildDate": "2026-04-20T14:02:11Z",
  "goVersion": "go1.26.3",
  "platform": "darwin/arm64"
}
```

---

## `kupe completion [shell]`

Generate shell completion scripts. Output goes to stdout; user is responsible for redirecting it into the shell's completion directory.

```bash
kupe completion zsh > "${fpath[1]}/_kupe"
kupe completion bash > /etc/bash_completion.d/kupe
kupe completion fish > ~/.config/fish/completions/kupe.fish
kupe completion powershell > $PROFILE.d/kupe.ps1
```

---

## `kupe auth` — Authentication

### `kupe auth login`

Authenticate and store credentials for the current or a new context.

**Flags:**

| Flag | Description |
|------|-------------|
| `--method METHOD` | `oidc` (default device-code flow) or `token` (long-lived API key). |
| `--token TOKEN` | API key to store when `--method token` is used. Required in non-interactive token mode. |
| `--tenant NAME` | Tenant to associate with this context. Prompts if not set. |
| `--api-url URL` | Override API base URL for this context. |
| `--oidc-base-url URL` | Override Authentik base URL for OIDC login. |
| `--oidc-client-id ID` | Override the OIDC public client ID. |
| `--context NAME` | Name for the context in the config file. Default: the tenant name. |
| `--set-default` | Make this context the current one. Default when only one context exists. |

**Examples:**

```bash
# Interactive OIDC device-code login
$ kupe auth login --tenant acme-corp
To finish signing in, open the following URL in any browser:

    https://auth.kupe.cloud/...

and enter this code:

    ABCD-EFGH

Logged in to tenant Acme Corp (acme-corp) as billy@acme.com.

# Scripted API-key bootstrap
$ kupe auth login --method token --tenant acme-corp --token "$KUPE_TOKEN" --context prod --set-default
```

Validates the resulting bearer token by calling `GET /api/v1/tenants/{tenant}`. Stores either the OIDC token set or API key in the OS keyring/plaintext fallback (see [auth.md](./auth.md)).

**Exit codes:** `0` on success, `3` on rejected credentials, `4` if the tenant doesn't exist.

### `kupe auth logout`

Remove credentials for a context.

**Flags:**

| Flag | Description |
|------|-------------|
| `--context NAME` | Context to log out of. Default: current. |
| `--all` | Log out of every context. |

**Examples:**

```bash
$ kupe auth logout
Logged out of "acme-corp".

$ kupe auth logout --context staging
$ kupe auth logout --all
```

Removes the token from the keyring (and from `~/.config/kupe/credentials.yaml` if the plaintext fallback was in use). Leaves the context itself in the config file — use `kupe config delete-context` to remove the full context.

### `kupe auth whoami`

Show the authenticated user, tenant, plan, context, API URL, and token storage source.

```
$ kupe auth whoami
User:    billy@acme.com
Tenant:  Acme Corp (acme-corp)
Plan:    pro
Context: acme-corp (https://api.kupe.cloud)
Storage: keyring
```

With `-o json`:

```json
{
  "user": "billy@acme.com",
  "tenant": "acme-corp",
  "tenantDisplayName": "Acme Corp",
  "plan": "pro",
  "context": "acme-corp",
  "apiUrl": "https://api.kupe.cloud",
  "storage": "keyring"
}
```

Backed by `GET /api/v1/tenants/{tenant}`. Exits `3` if the current context has no valid credentials.

### `kupe auth get-token`

Exec-plugin mode for Kubernetes. Emits a `client.authentication.k8s.io/v1` `ExecCredential` JSON to stdout. **Not intended for direct use** — called by `kubectl` when the kubeconfig was produced by `kupe cluster kubeconfig NAME --exec`.

**Flags:**

| Flag | Description |
|------|-------------|
| `--context NAME` | Which context's token to return. Set automatically by the kubeconfig that `--exec` produces. |

**Example output** (written to stdout, consumed by `kubectl`):

```json
{
  "apiVersion": "client.authentication.k8s.io/v1",
  "kind": "ExecCredential",
  "status": {
    "token": "kupe_abc_...",
    "expirationTimestamp": "2026-04-20T15:02:11Z"
  }
}
```

See the [Kubernetes client-go credential plugins docs](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins) for the full contract.

---

## `kupe config` — Configuration management

Reads and writes `~/.config/kupe/config.yaml`. Token values are never printed back (redacted in `view`).

### `kupe config view`

Print the current config. Tokens are redacted (`tokenRef: keyring` shown instead).

```bash
$ kupe config view
apiVersion: kupe.cloud/v1
kind: Config
currentContext: prod
contexts:
  - name: prod
    apiUrl: https://api.kupe.cloud
    tenant: acme-corp
    tokenRef: keyring
    user: billy@acme.com
    authMethod: oidc
  - name: staging
    apiUrl: https://api.staging.kupe.cloud
    tenant: acme-staging
    tokenRef: keyring
preferences:
  output: table
  wait: true
  waitTimeout: 30m
```

Use `-o json` for machine parseability.

### `kupe config current-context`

Print the name of the current context. Pipe-friendly.

```bash
$ kupe config current-context
prod
```

### `kupe config use-context NAME`

Switch the current context.

```bash
$ kupe config use-context staging
Switched to context "staging" (tenant acme-staging).
```

### `kupe config set-context NAME`

Create or update a context. If NAME exists, merge the provided fields.

**Flags:**

| Flag | Description |
|------|-------------|
| `--api-url URL` | API base URL. |
| `--tenant NAME` | Tenant. |
| `--token TOKEN` | Store token in keyring/plaintext fallback. |

```bash
$ kupe config set-context staging --api-url https://api.staging.kupe.cloud --tenant acme-staging
```

### `kupe config delete-context NAME`

Remove a context. Clears its token from keyring/plaintext.

```bash
$ kupe config delete-context old-tenant
Context "old-tenant" removed.
```

Prompts on TTY; requires `--yes` on non-TTY.

### `kupe config get KEY` / `kupe config set KEY VALUE`

Dotted-path access to the current context or preferences.

```bash
$ kupe config get contexts.prod.apiUrl
https://api.kupe.cloud

$ kupe config set preferences.waitTimeout 15m

$ kupe config get preferences.output
table
```

Invalid keys exit with code `2`.

---

## `kupe cluster` — Cluster lifecycle

### `kupe cluster list`

List clusters in the current tenant. Default table columns (from `GET /api/v1/tenants/{tenant}/clusters`):

```
NAME        TYPE     VERSION   PHASE        CPU   MEM    AGE
prod        shared   1.32      Running      4     16Gi   12d
staging     shared   1.32      Running      2     8Gi    5d
ephemeral   shared   1.32      Provisioning 2     4Gi    3m
```

With `-o wide`, adds `ENDPOINT`, `K8S-VERSION` (the actual running version from `status.kubernetesVersion`), and `STORAGE`.

With `-o name`, one cluster name per line — pipe-friendly for loops:

```bash
kupe cluster list -o name | xargs -I{} kupe cluster delete {} --yes
```

With `-o json`, full resource objects.

**Flags:**

| Flag | Description |
|------|-------------|
| `--phase PHASE` | Filter to a single phase (`Pending`, `Provisioning`, `Running`, `Degraded`, `Terminating`). Client-side filter. |

### `kupe cluster get NAME`

Get a single cluster. Default renders a compact details block; use `-o yaml` or `-o json` for the full resource.

```bash
$ kupe cluster get prod
Name:         prod
Display Name: Prod
Type:         shared
Version:      1.32 (running 1.32.3)
Phase:        Running
Endpoint:     https://api.prod.acme.kupe.cloud
Resources:
  CPU:        4
  Memory:     16Gi
  Storage:    100Gi
Created:      2026-04-08T09:00:00Z (12d ago)
```

Exits `4` if the cluster doesn't exist.

### `kupe cluster create NAME`

Create a new cluster. Waits for `status.phase=Running` by default (see [output.md](./output.md) for TTY/CI rendering).

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--type TYPE` | `shared` | `shared` or `dedicated`. Most users can omit this. |
| `--display-name NAME` | positional NAME | Human-readable name. |
| `--version VERSION` | latest | Target Kubernetes version (e.g., `1.32`). Defaults server-side. |
| `--cpu-limit QUANTITY` | required | CPU limit (e.g., `2`, `500m`, `1.5`). |
| `--memory-limit QUANTITY` | required | Memory limit (e.g., `8Gi`, `512Mi`). |
| `--storage-limit QUANTITY` | required | Storage limit (e.g., `50Gi`). |
| `--wait` | `true` | Wait for `status.phase=Running`. |
| `--wait-timeout DURATION` | `30m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | Output format: `table`, `json`, `yaml`, `go-template=...`, `jsonpath=...` (`jsonpath` currently returns a not-implemented error). |

Quantity formats match Kubernetes conventions and the regex constraints in [kupe-control-operator/api/v1alpha1/managedcluster_types.go](../../kupe-control-operator/api/v1alpha1/managedcluster_types.go) (`ClusterResources`).

Chain with `kupe cluster kubeconfig NAME --merge` to land a usable kubectl
context after the Running wait completes:

```bash
kupe cluster create prod --cpu-limit 2 --memory-limit 8Gi --storage-limit 50Gi \
  && kupe cluster kubeconfig prod --merge
```

**Example:**

```bash
$ kupe cluster create prod --version 1.32 \
    --cpu-limit 4 --memory-limit 16Gi --storage-limit 100Gi
Name:         prod
Display Name: prod
Type:         shared
Version:      1.32 (running 1.32.3)
Phase:        Running
Endpoint:     https://api.prod.acme.kupe.cloud
CPU:          4
Memory:       16Gi
Storage:      100Gi
Created:      2026-04-08T09:00:00Z (12d ago)
```

In CI/non-TTY, progress is written to stderr and the final resource render remains on stdout:

```
[00:00] Pending
[00:04] Provisioning
[02:04] Running
[02:04] cluster prod ready
```

**Exit codes:** `0` success, `5` on 409 (name exists), `8` on timeout, `1` on degraded.

### `kupe cluster update NAME`

Patch a cluster's version or resources. Uses ETag/If-Match internally (see [api-client.md](./api-client.md)).

**Flags:**

| Flag | Description |
|------|-------------|
| `--version VERSION` | New Kubernetes version. |
| `--cpu-limit QUANTITY` | New CPU limit. |
| `--memory-limit QUANTITY` | New memory limit. |
| `--storage-limit QUANTITY` | New storage limit. |
| `--if-match ETAG` | Require the given ETag; fail with `5` on mismatch. |
| `--force` | Skip the ETag read-modify-write cycle. |
| `--wait`, `--wait-timeout` | As per `create`. |
| `-o, --output FORMAT` | Same single-resource formats as `create`. |

```bash
$ kupe cluster update prod --version 1.33
Name:         prod
Version:      1.33 (running 1.33.0)
Phase:        Running
```

### `kupe cluster delete NAME`

Delete a cluster. Prompts on TTY; requires `--yes` on non-TTY.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--yes`, `-y` | off | Skip confirmation. |
| `--wait` | `true` | Wait for the resource to be gone (404). |
| `--wait-timeout` | `10m` | |

```bash
$ kupe cluster delete prod
? This will delete cluster "prod" (tenant acme-corp). Type the name to confirm: prod
```

`--wait=false` returns immediately with `cluster/prod delete requested` on stdout. The wait path uses stderr progress and does not print an extra stdout line after the resource is gone.

### `kupe cluster kubeconfig NAME`

Retrieve a kubeconfig for the cluster. Default: print full kubeconfig YAML to stdout.

The API endpoint returns only `{endpoint, certificateAuthority}`; the CLI assembles the full kubeconfig locally.

**Flags:**

| Flag | Description |
|------|-------------|
| `--merge` | Merge into `$KUBECONFIG` (or `~/.kube/config`) instead of printing. |
| `--context-name NAME` | Context name in the generated kubeconfig (default: `kupe-<tenant>-<cluster>`). |
| `--user-name NAME` | User entry name (default: same as context). |
| `--cluster-name NAME` | Cluster entry name (default: same as context). |
| `--exec` | Emit exec-plugin form that shells back to `kupe auth get-token`. |
| `--force` | Overwrite an existing context of the same name. |
| `--force-overwrite` | If the existing kubeconfig is corrupt, discard it and start fresh. |

**Examples:**

```bash
# Print to stdout
kupe cluster kubeconfig prod > /tmp/kc
KUBECONFIG=/tmp/kc kubectl get pods

# Merge into the standard kubeconfig
kupe cluster kubeconfig prod --merge
kubectl --context kupe-acme-corp-prod get pods

# Exec-plugin form (refreshes via `kupe auth get-token`)
kupe cluster kubeconfig prod --merge --exec
```

Exits `7` (Unavailable) if the cluster is not yet ready to return a kubeconfig — pair with `kupe cluster wait` in scripts.

### `kupe cluster wait NAME`

Block until the cluster reaches a target phase.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--for PHASE` | `running` | `running`, `pending`, `provisioning`, `upgrading`, `degraded`, `terminating`, or `deleted`. |
| `--timeout` | `30m` | Exit `8` if not reached. |

```bash
$ kupe cluster wait prod --for running --timeout 10m
cluster/prod running
```

Ctrl+C exits `130` cleanly with a hint.

---

## `kupe apikey` — API key management

Creates tenant-scoped API tokens. Keys are returned **once** at creation — there is no "show me the key again" endpoint, by design.

### `kupe apikey list`

List keys (metadata only — the secret portion is not retrievable after creation).

```
ID        NAME      ROLE      CREATED-BY       LAST-USED  AGE
8f2a3e7c… ci-prod   admin     billy@acme.com   3d ago     12d
c91b49aa… readonly  readonly  billy@acme.com   never      12d
```

With `-o wide`, adds `EXPIRES` and `ID-FULL`. With `-o json`, full metadata.

### `kupe apikey create`

Mint a new key. Prints the raw key once to stdout.

**Flags:**

| Flag | Description |
|------|-------------|
| `--name NAME` | Display name (required). |
| `--role ROLE` | `admin` or `readonly` (default `readonly`). |
| `--expires-at DATE` | RFC3339 date or duration (`30d`, `90d`). Default: never. |

```bash
$ kupe apikey create --name "CI Pipeline" --role admin --expires-at 90d

kupe_8f2a_k3mVb9xQpA7...
```

On a TTY, the CLI also prints metadata and a copy-now warning to stderr. With `-o json`, stdout contains the same data as JSON, including `"token"`.

**CI example:**

```bash
TOKEN=$(kupe apikey create --name ci-$GITHUB_SHA --role admin --expires-at 7d -o json | jq -r .token)
```

### `kupe apikey delete ID`

Revoke a key.

```bash
$ kupe apikey delete 8f2a3e7c-...
apikey/8f2a3e7c-... revoked
```

Prompts on TTY; requires `--yes` on non-TTY.

---

## `kupe secret` — Managed secret lifecycle

Manages `ManagedSecret` resources. Values live in OpenBao; the CLI handles
the metadata pointer (Vault path) and the list of clusters/namespaces the
operator mirrors the secret into.

### `kupe secret list`

List every managed secret in the tenant.

```
NAME         PHASE    SYNCS  AGE
mydb-pass    Active   1      3d
api-key      Active   2      12d
```

`-o wide` adds `PATH` (the Vault KV path) and `CLUSTERS` (deduped).

### `kupe secret get NAME`

Show details of a single secret.

### `kupe secret create NAME`

Create a new managed secret.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--path PATH` | — | Vault/OpenBao KV path where the secret value lives (required). |
| `--sync cluster:namespace[:secretName]` | none | Repeatable. Each target becomes a Kubernetes Secret mirrored into the named vcluster+namespace. If `secretName` is omitted, it defaults to NAME. |

```bash
kupe secret create mydb-pass --path kv/acme/mydb-pass \
  --sync prod:default \
  --sync staging:default:upstream-db-pass
```

Values themselves are not passed through the CLI — seed them into OpenBao
out-of-band.

### `kupe secret update NAME`

Replace a secret's sync list. Uses ETag RMW internally; a 412 mismatch is
retried once, then exits 5 on repeated contention.

At least one `--sync` is required. To clear all targets, delete and recreate
the managed secret without `--sync`.

```bash
kupe secret update mydb-pass --sync prod:default --sync staging:default
```

### `kupe secret delete NAME`

Delete a managed secret. Only the `ManagedSecret` resource and its mirrored
Kubernetes Secrets are removed — the underlying OpenBao value is untouched.

Prompts on TTY; requires `--yes` on non-TTY.

---

## `kupe member` — Tenant member management

Tenant members are email-identified users with a role (`admin` or `readonly`).
Changes propagate to Authentik groups via the kupe-control-operator.

### `kupe member list`

```
EMAIL              ROLE
alice@acme.com     admin
bob@acme.com       readonly
```

### `kupe member add EMAIL`

Add a user to the current tenant.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--role ROLE` | `readonly` | `admin` or `readonly`. |

### `kupe member update EMAIL`

Change a member's role. `--role` is required.

### `kupe member remove EMAIL`

Remove a user from the tenant. Prompts on TTY; requires `--yes` on non-TTY.
The user loses Authentik group membership on the next operator reconcile,
which revokes console and kubeconfig access.

---

## `kupe tenant` — Inspect the current tenant

`kupe tenant get` surfaces the full tenant record — plan, phase, resource pool,
current-period usage, and member list — for the context in play. Use it when
you need more than the thin identity check that `kupe auth whoami` provides.

Writes (rename, plan change, member changes) are not yet exposed; use `kupe
member` for membership edits, and the web console for the rest. A future
`kupe tenant update` may land once the operator stabilises PATCH ergonomics.

### `kupe tenant get`

```
$ kupe tenant get
Name:             acme
Display Name:     Acme Corp
Contact Email:    billing@acme.com
Plan:             pro
Phase:            Active
Cluster Count:    3
Pool:             CPU=8 MEM=32 STORAGE=200
Allocated:        CPU=4 MEM=16 STORAGE=100
Period:           2026-04-01 → 2026-05-01
Estimated Total:  156.15 GBP
Members:          2
Created:          2026-03-01T10:00:00Z (50d ago)
```

With `-o json` or `-o yaml`, the full tenant object is emitted including nested
`status.billing.currentUsage.compute` and `status.billing.currentUsage.observability`
blocks — use `jq .status.billing.currentUsage` to extract just the billing-relevant fields.

Backed by `GET /api/v1/tenants/{tenant}`. Exits `4` if the tenant (i.e. the
current context's tenant) no longer exists on the server, `3` on invalid
credentials.

---

## `kupe invoice` — Billing history

Read-only access to invoices. Billing is in arrears: usage charges and the
plan fee for a period land on the same invoice once the period closes.
Invoice names are server-controlled, typically `<tenant>-<YYYYMMDD>` for the
period start (final invoices issued on cancellation/deletion carry a
`-final` suffix). Run `kupe invoice list` before `kupe invoice get`.

All amounts are pre-tax; VAT/sales tax is added by Paddle at payment.

### `kupe invoice list`

```
$ kupe invoice list
NAME            PHASE  ISSUED                SUBTOTAL  CREDITS  TOTAL (EXCL. VAT)  CURRENCY
acme-20260301   Paid   2026-04-01T00:00:00Z  120.00    20.00    100.00             GBP
acme-20260201   Paid   2026-03-01T00:00:00Z  90.00     0.00     90.00              GBP
```

`-o wide` adds `TAX`, `START`, and `END` billing-period dates.

### `kupe invoice get NAME`

```
$ kupe invoice get acme-20260301
Name:               acme-20260301
Phase:              Paid
Issued:             2026-04-01T00:00:00Z
Period Start:       2026-03-01T00:00:00Z
Period End:         2026-04-01T00:00:00Z
Billed Until:       2026-04-01T00:00:00Z
Subtotal:           120.00
Credits Applied:    20.00
Tax:                0.00
Total (excl. VAT):  100.00 GBP
Line Items:         12 (use -o json for details)
```

Line items (per-resource usage rows with `kind` / `cost` / `quantity`) are
only meaningfully rendered as structured output — `-o json` or `-o yaml`
preserves every field the server emits.

```bash
kupe invoice get acme-20260301 -o json | jq '.status.lineItems[] | {kind, cost}'
```

Backed by `GET /api/v1/tenants/{tenant}/invoices[/{name}]`. `kupe invoice
get` exits `4` if the name has no invoice.

---

## `kupe plan` — Plan catalog

Plans are the public pricing tiers. The endpoint is unauthenticated on the
server (`/api/v1/plans`, not under `/tenants`), but the CLI still routes
through the current context for URL/TLS consistency — no token is required
for the request itself.

### `kupe plan list`

```
$ kupe plan list
NAME      DISPLAY   FEE    MAX-CLUSTERS  POOL
starter   Starter   0.00   2             -
pro       Pro       49.00  5             CPU=8 MEM=32 STORAGE=200
```

`-o wide` adds `METRICS-SERIES` and `LOG-GB` from the observability pool.

### `kupe plan get NAME`

```
$ kupe plan get pro
Name:              pro
Display Name:      Pro
Platform Fee:      49.00
Max Clusters:      5
Pool:              CPU=8 MEM=32 STORAGE=200
Active Series:     50000
Log Ingest (GB):   50
Retention (days):  90
Max Receivers:     10
```

With `-o json`, the full plan object including any future fields the server
adds. Exits `4` on an unknown plan name.

Useful in onboarding scripts (`kupe plan list -o name`) and CI smoke tests
(verify a plan exists before referencing it in a cluster-create call).

---

## Deferred beyond v1

These commands are not in v1 and will land in later phases:

- `kupe tenant update` — tenant PATCH (display name, contact email, plan
  change). Waiting on operator stabilising the PATCH contract; in the
  meantime the web console handles writes.
- `kupe alertmanager ...` — per-tenant alertmanager config (receivers,
  routes, global, templates). The API is live at
  `/api/v1/tenants/{tenant}/alertmanager/*` and the Terraform provider
  already covers it; the CLI mapping is planned for a later phase as a
  dedicated noun with subcommands:
  ```
  kupe alertmanager config get|set
  kupe alertmanager receiver list|get|set|delete
  kupe alertmanager route   list|get|set|delete
  kupe alertmanager global  get|set
  ```
  Design work here needs to settle on how receivers (raw YAML fragments)
  are piped in or templated — likely `--from-file=-` reading stdin, matching
  `kubectl create configmap`.
- `kupe tui` — k9s-like interactive view. See [tui.md](./tui.md).
