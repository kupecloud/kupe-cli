---
title: "Command Reference"
description: "Synopsis, flags, and examples for every kupe CLI subcommand in the v1 scope"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 3
---

This is the v1 command reference. Every command follows the grammar in [design.md](./design.md): `kupe <noun> <verb> [NAME] [--flags]`. Every command inherits the auth/config global flags described there. Output flags are local to commands because supported formats differ by command.

Commands in this document map one-to-one to endpoints in [kupe-api/api/swagger.json](../../kupe-api/api/swagger.json), plus the one self-service endpoint owned by the signup service (`kupe user delete`). Any endpoint not listed here is either in Phase 2 or intentionally not exposed via CLI.

## Coverage vs. kupe-api endpoints

| Endpoint | CLI command | Status |
|----------|-------------|--------|
| `GET /api/v1/tenants/{tenant}` | `kupe tenant get` (also `kupe auth whoami`) | v1 |
| `PATCH /api/v1/tenants/{tenant}` | — | deferred |
| `DELETE /api/v1/tenants/{tenant}` | `kupe tenant delete` | v1 |
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
| `GET /api/v1/tenants/{tenant}/databases` | `kupe database list` | v1 |
| `POST /api/v1/tenants/{tenant}/databases` | `kupe database create` | v1 |
| `GET /api/v1/tenants/{tenant}/databases/{name}` | `kupe database get` (also `kupe database wait`) | v1 |
| `PATCH /api/v1/tenants/{tenant}/databases/{name}` | `kupe database update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/databases/{name}` | `kupe database delete` | v1 |
| `POST /api/v1/tenants/{tenant}/databases/{name}/rotate-credentials` | `kupe database rotate-credentials` | v1 |
| `GET /api/v1/tenants/{tenant}/databases/{name}/bindings` | `kupe database binding list` | v1 |
| `POST /api/v1/tenants/{tenant}/databases/{name}/bindings` | `kupe database binding add` | v1 |
| `GET /api/v1/tenants/{tenant}/databases/{name}/bindings/{binding}` | `kupe database binding get` (also `binding update`, before its `PATCH`) | v1 |
| `PATCH /api/v1/tenants/{tenant}/databases/{name}/bindings/{binding}` | `kupe database binding update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/databases/{name}/bindings/{binding}` | `kupe database binding delete` | v1 |
| `GET /api/v1/tenants/{tenant}/registries` | `kupe registry list` | v1 |
| `POST /api/v1/tenants/{tenant}/registries` | `kupe registry create` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}` | `kupe registry get` (also `wait`, `delete`, `connect` and `logout`, which read it) | v1 |
| `PATCH /api/v1/tenants/{tenant}/registries/{name}` | `kupe registry update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}` | `kupe registry delete` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/bindings` | `kupe registry connection list` | v1 |
| `POST /api/v1/tenants/{tenant}/registries/{name}/bindings` | `kupe registry connect` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/bindings/{binding}` | `kupe registry connection get` (also `connection update` before its `PATCH`, and the waits) | v1 |
| `PATCH /api/v1/tenants/{tenant}/registries/{name}/bindings/{binding}` | `kupe registry connection update` | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}/bindings/{binding}` | `kupe registry disconnect` | v1 |
| `POST /api/v1/tenants/{tenant}/registries/{name}/bindings/{binding}/rotate` | `kupe registry connection rotate` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/repositories` | `kupe registry repo list` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}` | `kupe registry repo get` | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}` | `kupe registry repo delete` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}/images` | `kupe registry image list` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}/images/{reference}` | `kupe registry image get` | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}/images/{reference}` | `kupe registry image delete` | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}/repositories/{repository}/tags/{tag}` | `kupe registry tag delete` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/tokens` | `kupe registry token list` (also `logout`, for your sessions) | v1 |
| `POST /api/v1/tenants/{tenant}/registries/{name}/tokens` | `kupe registry token create` | v1 |
| `GET /api/v1/tenants/{tenant}/registries/{name}/tokens/{id}` | `kupe registry token get` (also `token create` and `login`, until it is issued) | v1 |
| `DELETE /api/v1/tenants/{tenant}/registries/{name}/tokens/{id}` | `kupe registry token revoke` (also `logout`) | v1 |
| `POST /api/v1/tenants/{tenant}/registries/{name}/sessions` | `kupe registry login` | v1 |
| `GET /api/v1/tenants/{tenant}/invoices` | `kupe invoice list` | v1 |
| `GET /api/v1/tenants/{tenant}/invoices/{name}` | `kupe invoice get` | v1 |
| `GET /api/v1/plans` | `kupe plan list` | v1 |
| `GET /api/v1/plans/{name}` | `kupe plan get` | v1 |
| `GET /api/v1/tenants/{tenant}/alertmanager/config` | — | planned (see below) |
| `GET,PUT,DELETE /api/v1/tenants/{tenant}/alertmanager/receivers[/{name}]` | — | planned (see below) |
| `GET,PUT,DELETE /api/v1/tenants/{tenant}/alertmanager/routes[/{id}]` | — | planned (see below) |
| `GET,PUT /api/v1/tenants/{tenant}/alertmanager/global` | — | planned (see below) |
| signup service `DELETE /users/me` | `kupe user delete` | v1 |

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
| `--signup-url URL` | Signup service base URL used by `kupe user delete` (default `https://signup.kupe.cloud`; env `KUPE_SIGNUP_URL`). |
| `--tenant NAME` | Tenant. |
| `--token TOKEN` | Store token in keyring/plaintext fallback. |

```bash
$ kupe config set-context staging --api-url https://api.staging.kupe.cloud --tenant acme-staging
$ kupe config set-context dev --api-url api.dev.int.kupe.cloud --signup-url signup.dev.int.kupe.cloud
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

Context keys: `apiUrl`, `tenant`, `tokenRef` (read-only), `user`, `signupUrl`.
Preference keys: `output`, `color`, `wait`, `waitTimeout`.

```bash
$ kupe config get contexts.prod.apiUrl
https://api.kupe.cloud

$ kupe config set contexts.dev.signupUrl signup.dev.int.kupe.cloud

$ kupe config set preferences.waitTimeout 15m

$ kupe config get preferences.output
table
```

Invalid keys exit with code `2`.

---

## Refusals shared by clusters and databases

Every `kupe cluster` and `kupe database` command maps the refusals both
services share the same way. The API's message is the error text; the hint is
the next step. kupe-api's error `code` decides; an older kupe-api that sends
only the platform's sentence is matched on that sentence instead.

| Refusal (`code`, HTTP status) | Exit | Hint |
| --- | --- | --- |
| Creation paused (`creation_paused`, 400) | `7` | retry later; status page link |
| Databases switched off on the platform (`databases_unavailable`, 404) | `4` | a platform setting: retrying will not help; existing databases can still be listed, unprotected and deleted, and their bindings deleted or narrowed |
| The cluster or database is being deleted (`terminating`, 409) | `5` | `kupe <noun> wait NAME --for deleted` |
| The tenant is being deleted (`tenant_terminating`, 409) | `5` | — |
| The tenant is suspended (`tenant_suspended`, 409) | `5` | contact support |
| No payment method yet (`payment_setup_required`, 409) | `5` | add one on the console's Billing page |
| The plan's count limit (`plan_limit_reached`, 400) | `5` | delete one or upgrade the plan |
| kupe-api temporarily unavailable (503, including "the tenant's database limit is still being set up") | `7` | retry in a few seconds |

The tenant refusals cover growing an existing cluster or database (a larger
size, high availability on) as well as creating one. Creates and updates are
never retried automatically: a 503 on one is reported with the hint, so a
script can decide to retry.

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
Endpoint:     https://prod.acme.clusters.kupe.cloud
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
| `--display-name NAME` | — | **Deprecated, ignored.** Clusters have no separate display name; NAME is used everywhere. |
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
Endpoint:     https://prod.acme.clusters.kupe.cloud
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

**Exit codes:** `0` success, `2` invalid flags or a spec the API rejected (the
hint names the flags to check), `5` on 409 (name exists) or a tenant or plan
refusal ([shared refusals](#refusals-shared-by-clusters-and-databases)), `7`
while the platform pauses cluster creation (`400 creation_paused`; the API's
message and a hint to retry later), on a 503, or when kupe-api stays
unreachable for 90 seconds while waiting ("lost contact with kupe-api while
waiting"), `8` on timeout,
`130` Ctrl+C while waiting (creation continues), `1` when the cluster enters
`Degraded` (`cluster "NAME" entered Degraded state`, hint: `kupe cluster get
NAME`).

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

By default the command waits until the change is in effect: the platform has
applied it (`status.observedGeneration` reaches the generation the change
produced) and the cluster is `Running`. A `Degraded` phase counts only once
the platform has applied the change, so a status read from before the change
never fails the wait. The platform can still report the failure from before
the change in the pass that applies it, so the wait rides out `Degraded` for 2
minutes (the platform's own grace for a recently healthy cluster) and exits
`1` if it lasts longer. A change the platform never applies ends in exit `8`
at `--wait-timeout`.

**Exit codes (waiting):** `1` when the cluster stays `Degraded` for 2 minutes
once the change is applied, or starts being deleted, `7` lost contact with
kupe-api, `8` on timeout, `130` Ctrl+C (the update continues).

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

**Exit codes:** `5` when the cluster kept changing while kupe-api deleted it
(hint: retry). Waiting: `7` lost contact with kupe-api, `8` on timeout, `130`
Ctrl+C (deletion continues).

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

Ctrl+C exits `130` cleanly with a hint. A cluster that enters `Degraded`
while waiting for another phase exits `1` (`--for degraded` succeeds instead);
kupe-api unreachable for 90 seconds exits `7`.

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

## `kupe database` — Databases

Alias: `kupe db`. Manages PostgreSQL databases and their bindings into
cluster namespaces. A database belongs to the tenant, not to a cluster:
deleting a cluster never deletes a database. Applications reach a database
through a **binding**, which delivers a Service named after the database and a
credentials Secret into namespaces of one cluster. The CLI never shows a
password.

Resources (`--cpu`, `--memory`, `--storage`) are **per instance** and are
billed at the standard unit rates: CPU and memory for every hour an instance is
running, storage for every hour its volume is bound. A highly available database
runs two instances, each with the requested resources.

When databases are not enabled on the platform, `list`, `get`,
`wait`, `delete`, `update NAME --deletion-protection=false`,
`binding list/get/delete` and `binding update --remove-namespace` keep working
so existing databases and bindings can still be cleaned up. Other
`kupe database` commands, and a `binding update` that adds a namespace, exit
`4` with the API's message, "databases are not available on this platform"
(`404 databases_unavailable`), and a hint that existing databases can still be
cleaned up. This is a platform setting, not an outage, so retrying does not
help.

### `kupe database list`

```
NAME        ENGINE    VERSION  RUNNING  PHASE    HA   AGE
orders-db   postgres  18       18.0     Running  off  3d
users-db    postgres  17       17.6     Running  on   12d
```

`VERSION` is the requested major; `RUNNING` is the full version the platform
reports. `-o wide` adds `CPU`, `MEMORY`, `STORAGE`, `INSTANCES` (ready/desired)
and `PROTECTED` (deletion protection). `-o name`, `-o json`, `-o yaml`,
`-o go-template=...` and `--phase` (a client-side filter, e.g. `--phase
degraded`) behave as for clusters.

### `kupe database get NAME`

```
$ kupe db get orders-db
Name:                 orders-db
Engine:               postgres
Version:              18 (running 18.0)
Phase:                Running
High Availability:    off
Instances:            1/1
CPU:                  500m
Memory:               1Gi
Storage:              10Gi
Deletion Protection:  on
Created:              2026-10-09T10:00:00Z (3d ago)
```

Exits `4` if the database doesn't exist.

### `kupe database create NAME`

Create a database and wait for `status.phase=Running` by default.

| Flag | Default | Description |
|------|---------|-------------|
| `--engine ENGINE` | `postgres` | Database engine. Only `postgres` is supported. |
| `--version MAJOR` | platform default | Engine major version (e.g. `18`). |
| `--cpu QUANTITY` | required | CPU per instance, without a unit (`500m`, `2`, `0.5`). |
| `--memory QUANTITY` | required | Memory per instance, with a unit (`1Gi`, `512Mi`). |
| `--storage QUANTITY` | required | Storage per instance, with a unit (`10Gi`). Grows later, never shrinks. |
| `--ha` | off | Two instances with a synchronous standby; resources are reserved and billed per instance. |
| `--no-deletion-protection` | off | Create without deletion protection (it is on by default). |
| `--wait` | `true` | Wait for `status.phase=Running`. |
| `--wait-timeout DURATION` | `30m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | Single-resource formats, as for `cluster create`. |

Platform floors and ceilings are enforced by the API; its message, including
any per-field reasons, is shown as returned. A name longer than the tenant may
use (`400 DATABASE_NAME_TOO_LONG`: at most 44 characters, fewer when the
tenant name is over 12, since the platform derives longer names from it) exits
`2` with the API's message and a hint to choose a shorter name. The plan's
database limit exits `5` with a hint. While
the platform pauses database creation (`400 creation_paused`) the command
exits `7` with the API's message and a hint to retry later. A `Degraded`
phase fails the wait.

```bash
kupe db create orders-db --cpu 500m --memory 1Gi --storage 10Gi
```

**Exit codes:** `0` success, `2` missing or malformed flags (no request made)
or an API validation error, `5` name exists or a tenant or plan refusal
([shared refusals](#refusals-shared-by-clusters-and-databases)), `7` creation
paused, a 503, or lost contact with kupe-api while waiting, `8` wait timeout,
`130` Ctrl+C while waiting (creation continues), `1` when the database enters
`Degraded` (`database "NAME" entered Degraded state`, hint: `kupe database
get NAME`).

### `kupe database update NAME`

Change per-instance resources, high availability or deletion protection. The
major version cannot be changed yet: kupe-api refuses every version change
(`400 major_upgrade_unavailable`), so `kupe database upgrade` is hidden until
major upgrades are offered. Only the flags you pass are sent (JSON merge-patch with `If-Match`; a `412` is
retried once, then exits `5`).

| Flag | Description |
|------|-------------|
| `--cpu`, `--memory`, `--storage` | New per-instance resources. Storage grows only. |
| `--ha` / `--ha=false` | Turn high availability on or off. |
| `--deletion-protection` / `--deletion-protection=false` | Turn deletion protection on or off. |
| `--wait` | Wait until the change is in effect (default `true`). |
| `--wait-timeout DURATION` | Give up after this long (default `30m`). Exits `8`. |
| `-o, --output FORMAT` | Single-resource formats. |

By default the command waits until the change is in effect: the platform has
applied it (`status.observedGeneration` reaches the generation the change
produced) and every instance runs it again (the `Ready` condition is `True`;
the phase stays `Running` while a change rolls out). A database with a single
instance is `Degraded` while that instance restarts for a CPU or memory
change; the wait rides that out, but exits `1` if the database stays
`Degraded` for 5 minutes, and at once if it starts being deleted. `Degraded`
counts only once the platform has applied the change, so a status read from
before the change never fails the wait; a change the platform never applies
ends in exit `8` at `--wait-timeout`. A change to
deletion protection alone is in effect at once (a delete reads it from the
spec) and is not waited on, so turning protection off on a `Degraded` or
never-provisioned database returns at once. Pass `--wait=false` to return as
soon as the change is accepted. A
smaller `--storage` (`400 storage_shrink_unsupported`) exits `2` with the
API's message, for example `storage can only grow: 5Gi is smaller than the
current 10Gi`, and a hint to check the current size with `kupe db get NAME`.
Any other refused change exits `2` with the API's message, which names the
field and the rule it broke.

**Exit codes (waiting):** `1` when the database stays `Degraded` for 5 minutes
once the change is applied, or starts being deleted, `7` lost contact with kupe-api, `8` on timeout, `130` Ctrl+C
(the change continues).

### `kupe database delete NAME`

Delete a database and its data. On a TTY you must type the database
name; `--yes` skips the prompt and is required in non-interactive mode.

Deleting a database removes it and its automatic backups immediately, and they cannot be restored. Manual backups stay until you delete them.

| Flag | Default | Description |
| --- | --- | --- |
| `--yes`, `-y` | off | Skip confirmation. |
| `--wait` | `true` | Wait for the database to be gone (404). |
| `--wait-timeout` | `15m` | Give up after this long. Exits `8`. |

By default the command waits until the database is gone: it is `Terminating`
while its bindings are removed from every cluster namespace and its instances
stop, which can take several minutes. As for clusters, the wait reports
progress on stderr and prints no stdout line once the database is gone.
`--wait=false` returns as soon as the request is accepted, with
`database/NAME delete requested` on stdout; follow it with
`kupe database wait NAME --for deleted`.

```
$ kupe db delete orders-db

Deleting database "orders-db" also removes its Service and credentials Secrets from
every bound cluster namespace.

Deleting a database removes it and its automatic backups immediately, and they cannot be restored. Manual backups stay until you delete them.

This will delete database "orders-db". Type the name to confirm: orders-db
✓ database orders-db deleted (01:42)
```

**Exit codes:** `0` gone (with `--wait=false`: accepted), `2` confirmation
missing or mismatched, `4` not found, `5` on `409 deletion_protected` (hint:
`kupe db update NAME --deletion-protection=false`) or when the database kept
changing while the delete was in flight (hint: retry), `7` lost contact with
kupe-api while waiting, `8` wait timeout, `130` Ctrl+C while waiting (deletion
continues).

### `kupe database rotate-credentials NAME --role owner|readwrite|readonly`

Request a new password for one role: `owner` (schema changes), `readwrite`
(read and write data) or `readonly` (read data), and by default wait until it
is in effect. Each role has its own password, so rotating one leaves the other
two unchanged. Once the rotation is in effect, the current password stops
working and every bound credentials Secret for the role carries the new one;
applications using the old password must restart.
Prompts on TTY (type `yes`); `--yes` is required in non-interactive mode.

| Flag | Default | Description |
| --- | --- | --- |
| `--role` | — | `owner`, `readwrite` or `readonly` (required). Any other value exits `2` before a request is sent. |
| `--yes`, `-y` | off | Skip confirmation. |
| `--wait` | `true` | Wait until the new password is in effect. |
| `--wait-timeout` | `10m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | `json`, `yaml` or `go-template=...` print `database`, `role`, `requestedAt`, `pending` and, after a wait, `appliedAt` and `delivered`. |

The rotation is in effect once `status.rotations.ROLE.appliedAt` (in
`kupe database get NAME -o yaml`) equals the `requestedAt` kupe-api returned
for the request and `status.rotations.ROLE.delivered` is `true`: the database
uses the new password and every binding of the role has delivered it to its
credentials Secret in every namespace the binding lists. The wait polls for
exactly that; a kupe-api that does not report `delivered` is taken at
`appliedAt` alone. The values are opaque tokens, compared for equality only.
A later rotation of the same role, requested by someone else, replaces this
one: the wait says so on stderr and waits for the newer rotation instead.
Delivery ends at the binding's Secrets on the platform side; the platform
copies them into your cluster after that, and the wait does not observe the
copy.
Stdout:
`database/NAME ROLE credentials rotated`, or with `--wait=false`
`database/NAME ROLE credentials rotation requested`. A kupe-api that does not
report `status.rotations`, or does not report the role asked for, cannot be
waited on: the command says so on stderr and prints the requested line.

**Exit codes:** `0` rotated (with `--wait=false`: requested), `2` bad
`--role` or confirmation missing, `4` not found, `5` when the database is being
deleted (`409 terminating`), `1` when it starts being deleted while waiting,
`7` lost contact with kupe-api while waiting, `8` the rotation is not in effect
by `--wait-timeout` (the platform keeps applying it; until `appliedAt` matches
the old password still works, and after that some bindings may not carry the
new one yet), `130` Ctrl+C while waiting (the rotation continues). Every hint
points at `status.rotations.ROLE`; `kupe database wait` does not wait for a
rotation.

### `kupe database wait NAME`

As `kupe cluster wait`: `--for running|pending|provisioning|upgrading|degraded|terminating|deleted`
(default `running`), `--timeout` (default `30m`, exit `8`).

### `kupe database binding` — Bind a database into cluster namespaces

A binding grants one role of one database to up to 10 namespaces of one
cluster. Each namespace gets a Service named after the database and a
credentials Secret (the same Secret name in every namespace), and a network
path to the database. `binding update` adds or removes namespaces; the
cluster, role and Secret name are fixed: to change one, delete the binding and
add another. Removing a namespace or a binding removes its Secrets, and the
Service and network path from each namespace no other binding of the database
lists in the same cluster. It does not remove knowledge of the password:
rotate the role afterwards if that matters.

| Command | Description |
|---------|-------------|
| `kupe db binding add DATABASE --cluster C --namespace NS [--namespace NS2 ...] --role owner\|readwrite\|readonly [--secret-name S] [--name N]` | Create a binding and wait for it to become `Ready` in every namespace (see below). `--namespace` repeats, 1 to 10 times; more exits `2` before a request is sent. `--name` defaults to `DATABASE-CLUSTER-ROLE` (must fit 63 characters); `--secret-name` defaults server-side to `DATABASE-ROLE`. The cluster may not exist yet. |
| `kupe db binding list DATABASE` | One row per namespace of each binding: `NAME`, `CLUSTER`, `NAMESPACE`, `ROLE`, `SECRET`, `PHASE`, `AGE`; `-o wide` adds the namespace's `HOST` and `MESSAGE`. `-o json` and `-o yaml` print the bindings as the API returns them. Exits `4` if the database doesn't exist. |
| `kupe db binding get DATABASE BINDING` | Details including the Secret name, then a table of its namespaces with each one's phase, in-cluster host and message. A binding of another database exits `4`. |
| `kupe db binding update DATABASE BINDING [--add-namespace NS]... [--remove-namespace NS]...` | Change the namespaces a binding lists and wait until every listed one is `Ready` and every removed one is withdrawn (see below). |
| `kupe db binding delete DATABASE BINDING` | Prompts on TTY (type `yes`); `--yes` in non-interactive mode. Asynchronous: stdout is `binding/NAME delete requested`. |

```bash
kupe db binding add orders-db --cluster prod --namespace orders-migrate --role owner
kupe db binding add orders-db --cluster prod --namespace orders-api --namespace orders-worker --role readwrite
kupe db binding add orders-db --cluster prod --namespace orders-reports --role readonly
```

```
$ kupe db binding list orders-db
NAME                      CLUSTER  NAMESPACE       ROLE       SECRET               PHASE    AGE
orders-db-prod-owner      prod     orders-migrate  owner      orders-db-owner      Ready    2m
orders-db-prod-readwrite  prod     orders-api      readwrite  orders-db-readwrite  Ready    2m
orders-db-prod-readwrite  prod     orders-worker   readwrite  orders-db-readwrite  Pending  2m
orders-db-prod-readonly   prod     orders-reports  readonly   orders-db-readonly   Ready    1m

$ kupe db binding get orders-db orders-db-prod-readwrite
Name:        orders-db-prod-readwrite
Database:    orders-db
Cluster:     prod
Namespaces:  orders-api, orders-worker
Role:        readwrite
Secret:      orders-db-readwrite
Phase:       Pending
Message:     the database is not yet available in namespace "orders-worker"; retrying
Created:     2026-10-18T10:00:00Z (2m ago)

NAMESPACE      PHASE    HOST                                    MESSAGE
orders-api     Ready    orders-db.orders-api.svc.cluster.local
orders-worker  Pending                                          the binding could not be reconciled; retrying
```

A namespace's `PHASE` is `Pending` until the platform reports it.

`binding add` waits for the binding by default:

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | `true` | Wait until `status.phase` is `Ready` and every listed namespace is `Ready` in `status.namespaces`. `--wait=false` returns as soon as the request is accepted. |
| `--wait-timeout DURATION` | `5m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | Single-resource formats. |

A binding is `Pending` while its database or cluster does not exist yet, is
not running yet or is being deleted. The platform keeps trying, and the binding
becomes `Ready` on its own once both are running. A wait that times out while
the binding is `Pending` exits `8` with the binding's status message, for
example `binding "orders-db-prod-readwrite" is still Pending: the database is
not yet available in namespace "orders-worker"; retrying`, and a hint to check
it with `kupe db binding get`.

A binding is `Failed` when one of its namespaces already has a Service named
after the database, or a Secret of the binding's Secret name, that the
platform did not create. The binding does not replace it. The wait stops at
once and exits `5` with the status message, the namespace and both names;
rename or remove that object and the binding becomes `Ready` on its own, or
delete the binding and add it again with another `--secret-name`. When the
binding also lists namespaces without a clash, the hint also gives the
`binding update --remove-namespace` command that stops binding into the
clashing ones.

```
$ kupe db binding add orders-db --cluster prod --namespace orders-api --role readwrite
Error: binding "orders-db-prod-readwrite" failed: a Secret named "orders-db-readwrite" already exists in namespace "orders-api", not created by the platform; remove it, or bind with another secretName
  namespace orders-api of cluster prod already has a Service "orders-db" or a Secret "orders-db-readwrite" that the platform did not create
  rename or remove that object, and the binding becomes Ready on its own
  or, if the Secret is the clash, choose another Secret name:  kupe database binding delete orders-db orders-db-prod-readwrite --yes
    then add the binding again with --secret-name
  check status:  kupe database binding get orders-db orders-db-prod-readwrite
```

**Exit codes (`binding add`):** `0` `Ready` (or accepted with `--wait=false`),
`2` missing or malformed flags, `4` database not found, `5` name exists or the
binding is `Failed` on a name conflict, `1` `Failed` for any other reason, `7`
lost contact with kupe-api while waiting, `8` wait timeout, `130` Ctrl+C while
waiting (the binding was created).

When the default name is taken, the binding of that role into that cluster
usually exists already: the hint gives the `binding update` command that adds
the namespaces to it, or says to pass `--name` for a second binding.

#### `kupe database binding update DATABASE BINDING`

Adds and removes namespaces of a binding. The command reads the binding,
edits its namespace list and sends the whole new list (`PATCH` with
`If-Match`); if the binding changed in between (`412`), it reads it again and
retries once. Adding a namespace that is already listed, or removing one that
is not, changes nothing: when nothing changes, no request is sent and stderr
says `binding "NAME" is unchanged: it lists orders-api, orders-worker`.

| Flag | Default | Description |
|------|---------|-------------|
| `--add-namespace NS` | — | Namespace to add; repeatable. Added at the end of the list. |
| `--remove-namespace NS` | — | Namespace to remove; repeatable. |
| `--wait` | `true` | Wait until every listed namespace is `Ready` and every removed one is gone from `status.namespaces`. `--wait=false` returns as soon as the change is accepted. |
| `--wait-timeout DURATION` | `5m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | Single-resource formats. |

A binding keeps 1 to 10 namespaces: a change that would leave none (use
`binding delete`) or more than 10 exits `2` before a request is sent. Admission
checks the namespaces added like `binding add` does: a namespace already bound
to the same role in that cluster, a Secret name already used there, or a
database whose bindings would list more than 20 namespaces is refused with the
platform's message. A change that only removes namespaces is never refused by
those checks. While a removed namespace is withdrawn, `binding list` and
`binding get` show it with `PHASE` `Removing`.

```bash
kupe db binding update orders-db orders-db-prod-readwrite --add-namespace orders-worker
kupe db binding update orders-db orders-db-prod-readwrite --remove-namespace orders-worker
```

**Exit codes (`binding update`):** `0` `Ready` in every listed namespace and
every removed one withdrawn (or accepted with `--wait=false`), `2` missing or
contradictory flags, a list that would be empty or over 10, or a change
admission refused, `4` database or binding not found, `5` the binding or its
database is being deleted (`409 terminating`), the binding kept changing
(`412` twice), or a listed namespace is `Failed` on a name conflict, `1`
`Failed` for any other reason, `7` lost contact with kupe-api while waiting,
`8` wait timeout, `130` Ctrl+C while waiting (the change was accepted).

---

## `kupe registry` — Container registry

The tenant's private container registry. A tenant has at most one registry
and it is always named `registry`, so no command takes a registry name or a
`--registry` flag. Images live at `registry.kupe.cloud/TENANT/REPOSITORY:TAG`.

| Command | What it does |
| --- | --- |
| `kupe registry create [--storage-limit 5Gi] [--no-deletion-protection]` | Turn the registry on and wait until it is `Running`. |
| `kupe registry get`, `kupe registry list` | Show it; `list` is a table of at most one row. |
| `kupe registry update [--storage-limit Q \| --clear-storage-limit] [--deletion-protection=true\|false]` | Change the storage limit or deletion protection. |
| `kupe registry delete [--yes]` | Delete it and every image in it. |
| `kupe registry wait --for ready\|deleted` | Wait for it to be `Running` or gone. |
| `kupe registry connect CLUSTER [--push-namespace NS]... [--secret-name NAME]` | Let every pod of a cluster pull; optionally give namespaces a push Secret. |
| `kupe registry connection list \| get CLUSTER \| update CLUSTER \| rotate CLUSTER` | Inspect connections, change push namespaces, rotate credentials. |
| `kupe registry disconnect CLUSTER [--yes]` | Remove a cluster's connection. |
| `kupe registry repo list \| get REPO \| delete REPO` | Browse and delete repositories (alias `repos`). |
| `kupe registry image list REPO \| get REF \| delete REPO@sha256:…` | Browse and delete images. |
| `kupe registry tag delete REPO:TAG` | Remove a tag; the image stays. |
| `kupe registry token create \| list \| get ID \| revoke ID` | Access tokens for automation; the list also shows login sessions. |
| `kupe registry login [--docker-config DIR] [--no-store -o json]` | Sign Docker in for 12 hours. |
| `kupe registry logout [--docker-config DIR]` | Remove the Docker credential and revoke your login sessions. |

### Refusals

The API's message is the error text; the hint is the next step.

| Refusal (`code`, HTTP status) | Exit | Hint |
| --- | --- | --- |
| No registry (404, `registry "registry" not found`; any 404 from `get`, `update`, `delete`, `wait`) | `4` | `kupe registry create` |
| Registries switched off on the platform (`registries_unavailable`, 404) | `4` | a platform setting, not an outage; an existing registry can still be listed, unprotected and deleted, and its connections removed |
| Deletion protection on (`deletion_protected`, 409) | `5` | `kupe registry update --deletion-protection=false` |
| Already has a registry (`REGISTRY_ALREADY_EXISTS`, 409) | `5` | `kupe registry get` |
| Cluster to connect does not exist (404, `cluster "CLUSTER" not found`) | `4` | `kupe cluster list` |
| Cluster already connected (`REGISTRY_BINDING_EXISTS`, 409) | `5` | `kupe registry connection get CLUSTER` |
| More than 10 push namespaces in a connection (`REGISTRY_PUSH_NAMESPACES_LIMIT`, 400), storage limit above the plan's (`REGISTRY_STORAGE_LIMIT_ABOVE_PLAN`, 400), expiry out of range (`REGISTRY_TOKEN_EXPIRY_INVALID`, 400) | `2` | — |
| More than 20 push namespaces across the registry's connections (`REGISTRY_PUSH_NAMESPACES_SERVICE_LIMIT`, 400) | `5` | remove one from another connection (`kupe registry connection list`) |
| Creating, updating or deleting the registry, a connection, a repository, an image or a tag, or creating an access token, by a member who is not an admin (403, `admin access required`) | `3` | — |
| Push token by a member who is not an admin (`REGISTRY_TOKEN_PUSH_ADMIN_ONLY`, 403) | `3` | ask a tenant admin to create it |
| Access token limit (`REGISTRY_TOKEN_LIMIT_REACHED`, 409) | `5` | `kupe registry token list --kind token` |
| Login session with an API key (`session_requires_user`, 403) | `3` | `kupe auth login`; for automation, `kupe registry token create` |
| Platform image of a multi-platform image (`image_in_use`, 409) | `5` | delete the multi-platform image that contains it |
| Image or tag the registry refuses to delete (`image_protected`, 409) | `5` | — |
| Registry not provisioned yet (`registry_not_ready`, 409) | `5` | `kupe registry wait --for ready` |
| The registry or the connection is being deleted (`terminating`, 409) | `5` | `kupe registry get` or `kupe registry connection get CLUSTER` |
| The plan includes no registry (`plan_limit_reached`, 400) | `5` | upgrade the plan |
| A repository, image, tag, connection or token that does not exist (404) | `4` | — (the message names it) |

The tenant refusals, creation paused and a 503 are the
[shared refusals](#refusals-shared-by-clusters-and-databases). Against a
kupe-api without registry routes every command gets the API's 404 and exits
`4`.

When the platform switches registries off, `list`, `get`, `wait`, `delete`,
`update --deletion-protection=false`, `connection list/get`, `disconnect`,
a `connection update` that only removes push namespaces, and every
repository, image and tag command keep working, so an existing registry can
be cleaned up. Repository, image and tag commands exit `4`
(`registries_unavailable`) only while kupe-api has no connection to the
registry at all.

### `kupe registry create`

| Flag | Default | Description |
| --- | --- | --- |
| `--storage-limit Q` | the plan's cap | Most the registry may store: a number with `Mi`, `Gi` or `Ti`, more than zero, at most 20 characters. Anything else exits `2` before a request. |
| `--no-deletion-protection` | off | Create without deletion protection. Without the flag no `deletionProtection` is sent and the platform's default, on, applies. |
| `--wait` | `true` | Wait for phase `Running`. |
| `--wait-timeout` | `15m` | Exits `8` after this long. |
| `-o, --output` | table | Single-resource formats. |

After the registry, stderr names its endpoint and the next steps
(`kupe registry login`, `kupe registry connect CLUSTER`).

### `kupe registry get` and `kupe registry list`

```
$ kupe registry get
Name:                 registry
Phase:                Running
Endpoint:             registry.kupe.cloud/acme
Storage:              1.2 GiB of 5.0 GiB
Storage Limit:        plan
Repositories:         2
Deletion Protection:  on
Created:              2026-10-21T10:00:00Z (21h ago)
```

`Storage` is what the registry stores, of the cap in force (`unlimited` on a
plan without one; usage alone until a cap is applied). `Storage Limit` is the
tenant's own limit, or `plan`. `list` shows `NAME`, `PHASE`, `ENDPOINT`,
`STORAGE`, `REPOSITORIES` and `AGE`; `-o wide` adds `PROTECTED` and `LIMIT`.

### `kupe registry update`

Only the flags passed are sent (`PATCH` with `If-Match`; a `412` is retried
once, then exits `5`). `--storage-limit` and `--clear-storage-limit` together,
or no flag at all, exit `2`: `nothing to update: pass at least one of
--storage-limit, --clear-storage-limit, --deletion-protection`.
`--deletion-protection` is sent only when given. The command does not wait:
the change is in effect when it returns.

### `kupe registry delete`

Prints "Deleting a registry deletes every image in it. They cannot be
restored." on stderr, then asks for the name to be typed (`registry`);
`--yes` skips both and is required in non-interactive mode. Deletion
protection is on by default and the command never turns it off: a protected
registry exits `5` with the hint
`kupe registry update --deletion-protection=false`. By default the command
waits until the registry is gone (`--wait-timeout`, default `15m`);
`--wait=false` prints `registry/registry delete requested`.

### `kupe registry wait`

`--for ready` (default; phase `Running`), `deleted`, `pending`,
`provisioning`, `degraded` or `terminating`; `--timeout` (default `30m`, exit
`8`). Prints `registry/registry running` or `registry/registry deleted`.

### `kupe registry connect CLUSTER`

Connects a cluster: every pod in it can pull from the registry with no
`imagePullSecret` of its own; the pull credential never enters the cluster.
`--push-namespace NS` (repeatable, up to 10) also gives the namespace a
Secret, named `--secret-name` (default `kupe-registry-push`, never
`kupe-registry-pull`, fixed once connected), that can pull from and push to
every repository in the registry, including replacing existing tags. Cluster and namespace names are checked
first (DNS labels; no `kube-*` or `argocd`; no repeats; at most 10), exit `2`;
kupe-api decides.

By default the command waits until the connection is `Ready`
(`--wait-timeout`, default `5m`), then prints on stderr:

```
Workloads in prod can now pull from registry.kupe.cloud/acme. Pods that were running before this connection was made keep their old settings: restart them to pull from the registry, for example "kubectl rollout restart deployment -n NAMESPACE NAME".
Each push namespace has a Secret named kupe-registry-push that can pull from and push to every repository in your registry, including replacing existing tags.
```

The second line appears only with push namespaces. The cluster must already
exist in your tenant: a missing one exits `4` with the hint `kupe cluster
list`. A wait that times out exits `8` with the connection's message. A connection is `Failed` when a push
namespace already has a Secret of that name the platform did not create (exit
`5` with what to change), or when the registry has more than 20 push
namespaces (exit `1` with the connection's message).

### `kupe registry connection`

| Command | Description |
| --- | --- |
| `connection list` | `CLUSTER`, `PHASE`, `PUSH-NAMESPACES`, `PULL-USERNAME`, `ROTATION` (`pending` while a rotation is applied), `AGE`; `-o wide` adds `PUSH-SECRET`, `PUSH-USERNAME`, `MESSAGE`. |
| `connection get CLUSTER` | The fields, then a table of push namespaces with each one's phase; a namespace being withdrawn shows `Removing`. |
| `connection update CLUSTER` | `--push-namespace NS`... replaces the list; `--add-push-namespace` / `--remove-push-namespace` edit it; `--clear-push-namespaces` sends `[]` (pull only). The modes do not mix (exit `2`). Read, edit and `PATCH` with `If-Match`, one retry on `412`. A change that would list more than 10, or a reserved namespace, exits `2` before the `PATCH`; no change sends nothing and says so on stderr. Waits until every listed namespace is `Ready` and every removed one withdrawn. |
| `connection rotate CLUSTER` | Prompts (type `yes`; `--yes` in non-interactive mode), requests new pull and push credentials and waits (`--wait-timeout`, default `10m`) until `status.rotation.appliedAt` equals the `requestedAt` returned. A newer rotation requested meanwhile replaces it, and the wait follows that one. Stdout: `connection/CLUSTER credentials rotated`, or `... rotation requested` with `--wait=false`; `-o json` prints `cluster`, `requestedAt`, `appliedAt`, `pending`. |

```
$ kupe registry connection list
CLUSTER  PHASE    PUSH-NAMESPACES  PULL-USERNAME           ROTATION  AGE
prod     Ready    ci               kupe+acme+pull-prod     none      3d
staging  Pending  none             kupe+acme+pull-staging  none      2m
```

### `kupe registry disconnect CLUSTER`

Prints "Pods in CLUSTER that restart after this cannot pull images from your
registry." and asks for `yes` (`--yes` skips both). Asynchronous: stdout is
`connection/CLUSTER delete requested`.

### `kupe registry repo`, `image` and `tag`

`REPO` is a repository name (`team-a/api`); an image `REF` is
`REPO:TAG`, `REPO@sha256:DIGEST` or both. The reference you push to,
`registry.kupe.cloud/TENANT/REPO:TAG`, is accepted too: a first segment with a
`.` or `:` followed by at least two more is taken as the host, and the tenant
after it must be the context's (another tenant's exits `2`). Upper-case
repository names, a bare `REPO` where an image is needed, and malformed
digests or tags exit `2` before a request. A repository is sent as one path
segment (`team-a%2Fapi`).

| Command | Description |
| --- | --- |
| `repo list [--search S] [--page N] [--page-size N] [--all]` | `REPOSITORY`, `IMAGES`, `PULLS`, `UPDATED`; wide adds `PULL-REFERENCE`, `CREATED`. 25 a page (at most 100); `--all` follows every page at 100. A table showing part of the list says on stderr how to see the rest. `IMAGES` counts each platform image of a multi-platform image as well as the multi-platform image. |
| `repo get REPO` | The repository and its pull reference. |
| `repo delete REPO` | Prints "Deleting a repository deletes every image and tag in it.", asks for the repository name to be typed (`--yes` skips). Stdout: `repository/REPO deleted`. |
| `image list REPO [--tagged \| --untagged] [--page N] [--page-size N] [--all]` | `DIGEST` (`sha256:` and 12 hex), `TAGS` (`untagged` when none), `SIZE` (binary units), `PLATFORMS`, `PUSHED`; wide adds `TYPE`, `LAST-PULLED`, `FULL-DIGEST`. `-o json` carries full digests. |
| `image get REF` | Digest, type, tags, size, platforms, and the references to pull by digest and by tag. |
| `image delete REPO@sha256:DIGEST` | By digest only: a tag exits `2` with `delete an image by digest; to remove a tag use "kupe registry tag delete REPO:TAG"`. Asks for `yes` (`--yes` skips). Stdout: `image/REPO@DIGEST deleted`. |
| `tag delete REPO:TAG` | Removes the tag; the image and its other tags stay. Asks for `yes`. Stdout: `tag/REPO:TAG deleted`. |

```
$ kupe registry image list team-a/api
DIGEST               TAGS      SIZE      PLATFORMS                 PUSHED
sha256:4f8c0d0e4b1f  v1        46.0 MiB  linux/amd64, linux/arm64  2h
sha256:bbbbbbbbbbbb  untagged  45.7 MiB  linux/amd64               9d
```

### `kupe registry token`

| Command | Description |
| --- | --- |
| `token create --name N --access pull\|push [--expires-in-days D]` | Creates an access token (admins only; push includes pull). `--expires-in-days` is 1 to 365 and sent only when given (the platform's default is 90); 0, 366 and other values outside the range exit `2`. Waits until the platform has issued the token (`--wait-timeout`, default `1m`), then prints its secret; a token not issued in time is revoked and the command exits `7`. |
| `token list [--kind token\|session\|all]` | `ID`, `NAME`, `KIND`, `ACCESS`, `CREATED-BY`, `EXPIRES`, `LAST-USED`; wide adds `PHASE`, `USERNAME`, `AGE`. Never a secret. |
| `token get ID` | One token or session. |
| `token revoke ID` | Asks for `yes` (`--yes` skips). Stdout: `token/ID revoked`. |

`token create` writes the secret (`kupecr_…`) alone to stdout, so
`TOKEN=$(kupe registry token create --name ci --access pull)` works. On a TTY,
stderr adds the ID, username, access, expiry and the command that signs
Docker in with it:

```
printf '%s' "$TOKEN" | docker login registry.kupe.cloud --username 'kupe+acme+tok-0123456789abcdef' --password-stdin
```

The username is always single-quoted and the secret is never part of the
command. `-o json` prints `id`, `name`, `access`, `username`, `registry`,
`secret`, `createdBy`, `createdAt`, `expiresAt` and `phase`.

### `kupe registry login`

1. Asks kupe-api for a login session: 12 hours, push for admins and pull for
   read-only members. A context signed in with an API key gets
   `session_requires_user` and exits `3` (hint: `kupe auth login`, or an access
   token for automation).
2. Waits until the platform has issued it (up to a minute) and uses the
   username it was issued under. Not issued in time: the session is revoked
   and the command exits `7`.
3. Stores the credential as `docker login` would. The Docker config is
   `--docker-config DIR/config.json`, else `$DOCKER_CONFIG/config.json`, else
   `~/.docker/config.json`. The helper is `credHelpers[HOST]`, else
   `credsStore`, else none; HOST is the registry's host, read from the
   registry first, as `logout` does. A helper (`docker-credential-NAME`, looked up on
   `PATH`, run without a shell, the secret on stdin) stores it, and the
   config's entry for the host is emptied; with no helper the config's `auths`
   entry holds it in base64, the file is written atomically at `0600` (never
   wider than it was), and stderr warns that it is unencrypted. Every other key
   of the config is kept. A helper name that is not letters, digits, `.`, `_`
   and `-` exits `2`; a helper not on `PATH` exits `1` naming it and the config
   key that chose it; both are checked before the session is requested. A
   helper's error output is shown with the secret replaced by `***`.
4. Prints on stderr: `Logged in to registry.kupe.cloud as 'USERNAME' until
   2026-10-22 22:00 BST (12 hours). Admins can push and pull; read-only members
   can pull. Run "kupe registry login" again after it expires: Docker reports
   "unauthorized" once it has.`

`--no-store -o json` prints `{"server", "username", "password", "expiresAt"}`
on stdout and stores nothing, for Podman, Buildah or a CI wrapper. `--no-store`
without `-o json`, or `-o json` without `--no-store`, exits `2`.

### `kupe registry logout`

Removes the credential for the registry host as `docker logout` would (the
helper's `erase`, and the config's `auths` entry), then revokes your own live
login sessions, on every machine. Access tokens are not touched. A session
that cannot be revoked is a warning on stderr; it still expires on its own.
A context with no signed-in user (an API key) revokes none and says so.

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

## `kupe tenant` — Inspect or delete the current tenant

`kupe tenant get` surfaces the full tenant record — plan, phase, resource pool,
current-period usage, and member list — for the context in play. Use it when
you need more than the thin identity check that `kupe auth whoami` provides.
`kupe tenant delete` is the owner-only, typed-name deletion.

Other writes (rename, plan change, member changes) are not yet exposed; use
`kupe member` for membership edits, and the web console for the rest. A future
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
Members:          2
Created:          2026-03-01T10:00:00Z (50d ago)
```

With `-o json` or `-o yaml`, the tenant object is emitted including the nested
`status.currentUsage.compute` and `status.currentUsage.observability` usage
quantities — use `jq .status.currentUsage` to extract just the usage. No output
format shows an amount.

Backed by `GET /api/v1/tenants/{tenant}`. Exits `4` if the tenant (i.e. the
current context's tenant) no longer exists on the server, `3` on invalid
credentials.

### `kupe tenant delete --confirm NAME`

Permanently delete the current context's tenant. Only the tenant **owner**
(`Contact Email` in `kupe tenant get`) may do this, and only with an OIDC
login — the CLI refuses API-key contexts and `kupe_…` tokens up front (exit
`3`) because the API would reject them with `403 owner_required` anyway.

There is no interactive prompt: `--confirm` must equal the current context's
tenant name (mismatch or missing → exit `2`, no request made).

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--confirm NAME` | required | Tenant name, typed out. Must equal the context's tenant. |
| `--cascade` | off | Delete every cluster in the tenant first. Without it a tenant that still has clusters is refused (`409 clusters_exist`). The clusters that will go are listed on stderr (from `GET /clusters`) before the request. `--cascade` never deletes databases or the registry. |
| `--wait` | off | Poll `GET /api/v1/tenants/{tenant}` until it returns `404`. |
| `--wait-timeout DURATION` | `30m` | Give up waiting after this long. Exits `8`. |

```bash
$ kupe tenant delete --confirm acme --cascade

Deleting tenant "acme" will:
  - Remove every member's access, all API keys, and all managed secrets.
  - Issue a final invoice for any outstanding usage.
  - Delete 2 cluster(s), including every workload and volume inside them:
      prod (Running)
      staging (Running)
  - Not delete databases: --cascade never does, and the tenant is refused while it has any.

tenant/acme terminating
```

A tenant that still has databases is refused with or without `--cascade`
(`409 databases_exist`). Delete each database first: `kupe db list`, then
`kupe db update NAME --deletion-protection=false` and `kupe db delete NAME`.
A tenant that still has a registry is refused the same way
(`409 registries_exist`): `kupe registry update --deletion-protection=false`,
then `kupe registry delete`.

The warning block goes to stderr; `tenant/<name> terminating` is the only
stdout line. Deletion is asynchronous — the API answers `202` and
`kupe tenant get` shows `Phase: Terminating` until the tenant is gone (then
exits `4`). A tenant that was ever billed is held for its final invoice, up to
24 hours, so `--wait` is usually only useful with a generous `--wait-timeout`.

**Exit codes:** `0` accepted (with `--wait`: gone), `2` missing/mismatched
`--confirm` (or `400` from the API), `3` API-key context or `403 owner_required`,
`4` tenant not found, `5` on `409 clusters_exist` (hint: `--cascade`),
`409 databases_exist` (hint: delete each database first),
`409 registries_exist` (hint: delete the registry first) or
`409 already_terminating`, `6` on `429` (one request per minute), `8` `--wait`
timeout, `130` Ctrl+C while waiting (deletion continues server-side).

Backed by `DELETE /api/v1/tenants/{tenant}` with body
`{"confirm": "<name>", "cascade": <bool>}`.

---

## `kupe user` — Your own account

Self-service operations on the logged-in user. These go to the **signup
service** (`https://signup.kupe.cloud`, per-context `signupUrl`, env
`KUPE_SIGNUP_URL`), not kupe-api, and require an OIDC login: an API key
identifies a tenant, not a person.

### `kupe user delete --confirm EMAIL`

Permanently erase your Kupe user: the Authentik account (sessions and tokens
with it) and the Grafana user row. Idempotent server-side; cannot be undone.

Preconditions the CLI checks before calling out:

- The current context (or `--token`/`KUPE_API_TOKEN`) is an OIDC login —
  API-key contexts and `kupe_…` tokens exit `3`.
- `--confirm` equals the logged-in email recorded at login (`kupe auth
  whoami` → `User:`), compared case-insensitively; mismatch exits `2`. When
  the email is not known locally (token supplied via env/flag) the CLI says
  so and lets the service verify it against the token.

The service additionally requires that you belong to **no** tenant, and that
the login is recent (`auth_time` within 10 minutes).

**Flags:**

| Flag | Description |
|------|-------------|
| `--confirm EMAIL` | Your email address, typed out (required). |

```bash
$ kupe user delete --confirm billy@acme.com

Deleting user "billy@acme.com" will:
  - Permanently remove the account, its sessions and tokens, and its Grafana user.
  - Log this CLI out of the current context.

Logged out of "acme".
user/billy@acme.com deleted
```

On `204` the CLI removes the stored credentials for the current context
(the `kupe auth logout` equivalent; the context entry itself is kept) and
prints the single stdout line `user/<email> deleted`. A failed request never
touches stored credentials.

**Exit codes:** `0` deleted, `2` missing/mismatched `--confirm` (or `400`
from the service), `3` API-key auth or `403` (not an OIDC token, stale
login, refused identity — re-run `kupe auth login` and retry within 10
minutes), `5` on `409 tenant_memberships` — the message lists the tenants to
delete (`kupe tenant delete`) or be removed from (`kupe member remove`)
first, `6` on `429` (one request per minute).

Backed by the signup service's `DELETE /users/me` with body
`{"confirm": "<email>"}` and the OIDC bearer token.

---

## `kupe invoice` — Billing history

Read-only access to invoices. Billing is in arrears: a period's usage lands
on one invoice once the period closes.
Invoice names are server-controlled, usually `<tenant>-<YYYYMMDD>` for the
period start, but variants exist: final invoices issued on
cancellation/deletion carry a `-final` suffix, and a timestamp-suffixed
form (`<tenant>-<YYYYMMDD-HHMMSS>`) is used when two periods start on the
same date. Always list invoices rather than constructing names — run
`kupe invoice list` before `kupe invoice get`.

No output format shows an amount: no totals, tax, credits or prices.

### `kupe invoice list`

```
$ kupe invoice list
NAME            PHASE  ISSUED
acme-20260301   Paid   2026-04-01T00:00:00Z
acme-20260201   Paid   2026-03-01T00:00:00Z
```

`-o wide` adds the `START` and `END` billing-period dates.

### `kupe invoice get NAME`

```
$ kupe invoice get acme-20260301
Name:          acme-20260301
Phase:         Paid
Issued:        2026-04-01T00:00:00Z
Period Start:  2026-03-01T00:00:00Z
Period End:    2026-04-01T00:00:00Z
Billed Until:  2026-04-01T00:00:00Z
Line Items:    12 (use -o json for details)
```

Line items (per-resource usage rows with `description`, `cluster`,
`resourceType` and `quantity`) are only meaningfully rendered as structured
output — `-o json` or `-o yaml`. They carry no price or amount.

```bash
kupe invoice get acme-20260301 -o json | jq '.status.lineItems[] | {resourceType, quantity}'
```

Backed by `GET /api/v1/tenants/{tenant}/invoices[/{name}]`. `kupe invoice
get` exits `4` if the name has no invoice.

---

## `kupe plan` — Plan catalog

Plans are the platform tiers. The endpoint is unauthenticated on the
server (`/api/v1/plans`, not under `/tenants`), but the CLI still routes
through the current context for URL/TLS consistency — no token is required
for the request itself.

### `kupe plan list`

```
$ kupe plan list
NAME     DISPLAY  MAX-CLUSTERS  MAX-DATABASES  MAX-REGISTRY-STORAGE  POOL
starter  Starter  2             1              -                     -
pro      Pro      5             5              5Gi                   CPU=8 MEM=32 STORAGE=200
```

`-o wide` adds `METRICS-SERIES` and `LOG-GB` from the observability pool.

`0` under `MAX-CLUSTERS`, `MAX-DATABASES` or `MAX-REGISTRY-STORAGE` means
the plan sets no limit. `-` under `MAX-DATABASES` means the server did not
report a database limit for the plan; json and yaml output leave the field
out in that case. `MAX-REGISTRY-STORAGE` is the most a tenant's registry may
store, in binary units (`5Gi`); `-` there means the server reported no
registry allowance (the plan includes no registry), and json and yaml output
leave `registry` out.

### `kupe plan get NAME`

```
$ kupe plan get pro
Name:                  pro
Display Name:          Pro
Max Clusters:          5
Max Databases:         5
Max Registry Storage:  5Gi
Resource Pool:         CPU=8 MEM=32 STORAGE=200
Active Series:         50000
Log Ingest (GB):       50
Retention (days):      90
Max Receivers:         10
```

With `-o json` or `-o yaml`, the fields shown above under the API's names
(the registry cap is `registry.maxStorage`); a field the CLI does not read is
left out. No output format shows a plan fee or any other amount. Exits `4`
on an unknown plan name.

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
