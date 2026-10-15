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
| `PATCH /api/v1/tenants/{tenant}/databases/{name}` | `kupe database update`, `kupe database upgrade` | v1 |
| `DELETE /api/v1/tenants/{tenant}/databases/{name}` | `kupe database delete` | v1 |
| `POST /api/v1/tenants/{tenant}/databases/{name}/rotate-credentials` | `kupe database rotate-credentials` | v1 |
| `GET /api/v1/tenants/{tenant}/databases/{name}/bindings` | `kupe database binding list` | v1 |
| `POST /api/v1/tenants/{tenant}/databases/{name}/bindings` | `kupe database binding add` | v1 |
| `GET /api/v1/tenants/{tenant}/databases/{name}/bindings/{binding}` | `kupe database binding get` | v1 |
| `DELETE /api/v1/tenants/{tenant}/databases/{name}/bindings/{binding}` | `kupe database binding delete` | v1 |
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
| Databases switched off on the platform (`databases_unavailable`, 404) | `4` | a platform setting: retrying will not help; existing databases can still be listed, unprotected and deleted |
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

**Exit codes (waiting):** as for `create`: `1` when the cluster enters
`Degraded` or starts being deleted, `7` lost contact with kupe-api, `8` on
timeout, `130` Ctrl+C (the update continues).

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
credentials Secret into one namespace of one cluster. The CLI never shows a
password.

Resources (`--cpu`, `--memory`, `--storage`) are **per instance** and are
billed at the standard unit rates for every hour the database exists. A highly
available database runs two instances, each with the requested resources.

When databases are not enabled on the platform, `list`, `get`,
`wait`, `delete`, `update NAME --deletion-protection=false` and
`binding list/get/delete` keep working so existing databases can still be
cleaned up. Other `kupe database` commands exit `4` with the API's message,
"databases are not available on this platform" (`404 databases_unavailable`),
and a hint that existing databases can still be cleaned up. This is a platform
setting, not an outage, so retrying does not help.

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
any per-field reasons, is shown as returned. The plan's database limit exits
`5` with a hint. While
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

Change per-instance resources, high availability or deletion protection. Only
the flags you pass are sent (JSON merge-patch with `If-Match`; a `412` is
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
`Degraded` for 5 minutes, and at once if it starts being deleted. Pass
`--wait=false` to return as soon as the change is accepted. A
smaller `--storage` (`400 storage_shrink_unsupported`) exits `2` with the
API's message, for example `storage can only grow: 5Gi is smaller than the
current 10Gi`, and a hint to check the current size with `kupe db get NAME`.
Any other refused change exits `2` with the API's message, which names the
field and the rule it broke.

**Exit codes (waiting):** `1` when the database stays `Degraded` or starts
being deleted, `7` lost contact with kupe-api, `8` on timeout, `130` Ctrl+C
(the change continues).

### `kupe database upgrade NAME --version MAJOR`

Request an in-place major version upgrade (upward only; the database is
offline while it runs). When the platform does not offer the upgrade
(`400 major_upgrade_unavailable`) the command exits `7` with the API's
message. Requesting the current major is a no-op.

### `kupe database delete NAME`

Delete a database and its data. On a TTY you must type the database
name; `--yes` skips the prompt and is required in non-interactive mode.

Deleting a database removes it immediately. Its storage is kept for 7 days for support-assisted recovery, then permanently deleted. There is no self-service restore in v1.

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

Deleting a database removes it immediately. Its storage is kept for 7 days for support-assisted recovery, then permanently deleted. There is no self-service restore in v1.

This will delete database "orders-db". Type the name to confirm: orders-db
✓ database orders-db deleted (01:42)
```

**Exit codes:** `0` gone (with `--wait=false`: accepted), `2` confirmation
missing or mismatched, `4` not found, `5` on `409 deletion_protected` (hint:
`kupe db update NAME --deletion-protection=false`) or when the database kept
changing while the delete was in flight (hint: retry), `7` lost contact with
kupe-api while waiting, `8` wait timeout, `130` Ctrl+C while waiting (deletion
continues).

### `kupe database rotate-credentials NAME --role owner|app`

Request a new password for one role: `owner` (schema changes) or `app` (read
and write data), and by default wait until it is in effect. Once it is, the
current password stops working and every bound credentials Secret for the role
carries the new one; applications using the old password must restart.
Prompts on TTY (type `yes`); `--yes` is required in non-interactive mode.

| Flag | Default | Description |
| --- | --- | --- |
| `--role` | — | `owner` or `app` (required). |
| `--yes`, `-y` | off | Skip confirmation. |
| `--wait` | `true` | Wait until the new password is in effect. |
| `--wait-timeout` | `10m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | `json`, `yaml` or `go-template=...` print `database`, `role`, `requestedAt`, `pending` and, after a wait, `appliedAt`. |

The rotation is in effect once `status.rotations.ROLE.appliedAt` (in
`kupe database get NAME -o yaml`) equals the `requestedAt` kupe-api returned
for the request; the wait polls for exactly that. The values are opaque
tokens, compared for equality only, so a later rotation requested by someone
else replaces this one and the wait then times out. Stdout:
`database/NAME ROLE credentials rotated`, or with `--wait=false`
`database/NAME ROLE credentials rotation requested`. A kupe-api that does not
report `status.rotations` yet cannot be waited on: the command says so on
stderr and prints the requested line.

**Exit codes:** `0` rotated (with `--wait=false`: requested), `2` bad
`--role` or confirmation missing, `4` not found, `5` when the database is being
deleted (`409 terminating`), `1` when it starts being deleted while waiting,
`7` lost contact with kupe-api while waiting, `8` the rotation is not in effect
by `--wait-timeout` (the old password still works; the platform keeps
applying it), `130` Ctrl+C while waiting (the rotation continues).

### `kupe database wait NAME`

As `kupe cluster wait`: `--for running|pending|provisioning|upgrading|degraded|terminating|deleted`
(default `running`), `--timeout` (default `30m`, exit `8`).

### `kupe database binding` — Bind a database into a cluster namespace

A binding grants one role of one database to one namespace of one cluster. All
fields are fixed; to change one, delete the binding and add another. Removing a
binding removes the Service, the Secret and the network path, but not
knowledge of the password: rotate the role afterwards if that matters.

| Command | Description |
|---------|-------------|
| `kupe db binding add DATABASE --cluster C --namespace NS --role owner\|app [--secret-name S] [--name N]` | Create a binding and wait for it to become `Ready` (see below). `--name` defaults to `DATABASE-CLUSTER-NAMESPACE-ROLE` (must fit 63 characters); `--secret-name` defaults server-side to `DATABASE-ROLE`. The cluster may not exist yet. |
| `kupe db binding list DATABASE` | `NAME`, `CLUSTER`, `NAMESPACE`, `ROLE`, `SECRET`, `PHASE`, `AGE`; `-o wide` adds `HOST` and `MESSAGE`. Exits `4` if the database doesn't exist. |
| `kupe db binding get DATABASE BINDING` | Details including the in-cluster host and the Secret name. A binding of another database exits `4`. |
| `kupe db binding delete DATABASE BINDING` | Prompts on TTY (type `yes`); `--yes` in non-interactive mode. Asynchronous: stdout is `binding/NAME delete requested`. |

```bash
kupe db binding add orders-db --cluster prod --namespace api --role owner --secret-name orders-db-migrate
kupe db binding add orders-db --cluster prod --namespace api --role app
```

`binding add` waits for the binding by default:

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | `true` | Wait for `status.phase=Ready`. `--wait=false` returns as soon as the request is accepted. |
| `--wait-timeout DURATION` | `5m` | Give up after this long. Exits `8`. |
| `-o, --output FORMAT` | table | Single-resource formats. |

A binding is `Pending` while its database or cluster does not exist yet, is
not running yet or is being deleted. The platform keeps trying, and the binding
becomes `Ready` on its own once both are running. A wait that times out while
the binding is `Pending` exits `8` with the binding's status message, for
example `binding "orders-db-prod-api-app" is still Pending: cluster prod does
not exist`, and a hint to check it with `kupe db binding get`.

A binding is `Failed` when the namespace already has a Service named after the
database, or a Secret of the binding's Secret name, that the platform did not
create. The binding does not replace it. The wait stops at once and exits `5`
with the status message and both names; rename or remove that object and the
binding becomes `Ready` on its own, or delete the binding and add it again with
another `--secret-name`.

```
$ kupe db binding add orders-db --cluster prod --namespace api --role app
Error: binding "orders-db-prod-api-app" failed: a Secret named "orders-db-app" already exists in namespace "api", not created by the platform; remove it, or bind with another secretName
  namespace api of cluster prod already has a Service "orders-db" or a Secret "orders-db-app" that the platform did not create
  rename or remove that object, and the binding becomes Ready on its own
  or, if the Secret is the clash, choose another Secret name:  kupe database binding delete orders-db orders-db-prod-api-app --yes
    then add the binding again with --secret-name
  check status:  kupe database binding get orders-db orders-db-prod-api-app
```

**Exit codes (`binding add`):** `0` `Ready` (or accepted with `--wait=false`),
`2` missing or malformed flags, `4` database not found, `5` name exists or the
binding is `Failed` on a name conflict, `1` `Failed` for any other reason, `7`
lost contact with kupe-api while waiting, `8` wait timeout, `130` Ctrl+C while
waiting (the binding was created).

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
| `--cascade` | off | Delete every cluster in the tenant first. Without it a tenant that still has clusters is refused (`409 clusters_exist`). The clusters that will go are listed on stderr (from `GET /clusters`) before the request. `--cascade` never deletes databases. |
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

The warning block goes to stderr; `tenant/<name> terminating` is the only
stdout line. Deletion is asynchronous — the API answers `202` and
`kupe tenant get` shows `Phase: Terminating` until the tenant is gone (then
exits `4`). A tenant that was ever billed is held for its final invoice, up to
24 hours, so `--wait` is usually only useful with a generous `--wait-timeout`.

**Exit codes:** `0` accepted (with `--wait`: gone), `2` missing/mismatched
`--confirm` (or `400` from the API), `3` API-key context or `403 owner_required`,
`4` tenant not found, `5` on `409 clusters_exist` (hint: `--cascade`),
`409 databases_exist` (hint: delete each database first) or
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

Read-only access to invoices. Billing is in arrears: usage charges and the
plan fee for a period land on the same invoice once the period closes.
Invoice names are server-controlled, usually `<tenant>-<YYYYMMDD>` for the
period start, but variants exist: final invoices issued on
cancellation/deletion carry a `-final` suffix, and a timestamp-suffixed
form (`<tenant>-<YYYYMMDD-HHMMSS>`) is used when two periods start on the
same date. Always list invoices rather than constructing names — run
`kupe invoice list` before `kupe invoice get`.

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
