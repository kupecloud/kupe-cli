---
title: "Authentication & Config"
description: "Token lifecycle, config file schema, keyring storage, precedence, and exec-plugin contract for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 4
---

The CLI authenticates against `kupe-api` using a tenant-scoped bearer token. This doc specifies how tokens are stored, resolved, rotated, and surfaced to `kubectl` via the exec-plugin contract.

## Token model

Every token is an API key minted against a specific tenant. Format: `kupe_<keyID>_<secret>` (prefix `kupe_` for routing on the server side, ID for auditability, secret for auth). The API validates via constant-time HMAC-SHA256 comparison against a stored hash (see [kupe-api/internal/auth/auth.go](../../kupe-api/internal/auth/auth.go)).

Two roles exist: `admin` (read-write) and `readonly`. The role is a property of the key, not the user — a tenant member can create keys in either role as long as their own role allows it.

OIDC JWTs are **not supported in v1**. OIDC device flow is Phase 1.5, pending Authentik exposing a device-code grant (see [Open questions](#open-questions)).

## Config file

### Location

| OS | Default path | Override |
|----|--------------|----------|
| macOS | `~/Library/Application Support/kupe/config.yaml` (respect `XDG_CONFIG_HOME`) | `$KUPE_CONFIG` or `--config` |
| Linux | `$XDG_CONFIG_HOME/kupe/config.yaml` or `~/.config/kupe/config.yaml` | `$KUPE_CONFIG` or `--config` |
| Windows | `%AppData%\kupe\config.yaml` | `$KUPE_CONFIG` or `--config` |

For consistency with kubectl and gh, Linux users on most distros will see `~/.config/kupe/config.yaml`. macOS falls back to the same path unless they've set `XDG_CONFIG_HOME`.

File mode is `0600` on creation. Directory mode is `0700`.

### Schema

```yaml
apiVersion: kupe.cloud/v1
kind: Config
currentContext: prod
contexts:
  - name: prod
    apiUrl: https://api.kupe.cloud
    tenant: acme-corp
    tokenRef: keyring          # "keyring" | "plaintext"
    user: billy@acme.com       # populated on login — display hint only
  - name: staging
    apiUrl: https://api.staging.kupe.cloud
    tenant: acme-staging
    tokenRef: keyring
    user: billy@acme.com
preferences:
  output: table                # table | wide | json | yaml | name
  color: auto                  # auto | always | never
  wait: true
  waitTimeout: 30m
```

**Tokens are never written into this file.** `tokenRef` is a pointer to where the token actually lives (the OS keyring, or the plaintext-fallback credentials file). See [Token storage](#token-storage).

Unknown top-level keys produce a loud error pointing at `kupe config view`. Unknown keys inside `preferences` are ignored with a warning on `-v`.

### Credentials file (plaintext fallback only)

When keyring is unavailable, tokens live in a separate file:

```
~/.config/kupe/credentials.yaml   (mode 0600)
```

```yaml
apiVersion: kupe.cloud/v1
kind: Credentials
tokens:
  prod: "kupe_8f2a_k3mVb9xQpA7..."
  staging: "kupe_c91b_f7Gh4j8kLm..."
```

This file is **separate** from `config.yaml` so an operator can safely `cat ~/.config/kupe/config.yaml` in a support channel without leaking secrets.

## Token storage

### Keyring (default)

Via [`github.com/zalando/go-keyring`](https://github.com/zalando/go-keyring):

| OS | Backend |
|----|---------|
| macOS | Keychain (`/usr/bin/security`) |
| Linux | Secret Service (libsecret — GNOME Keyring, KDE Wallet, KeePassXC) |
| Windows | Credential Manager (`wincred`) |

Service key: `kupe-cli`. Account key: the context name (e.g., `prod`). A single binary invocation touches exactly one keyring entry.

On login, the CLI writes to the keyring and sets `tokenRef: keyring` in the config. On logout, it deletes the keyring entry.

### Plaintext fallback

Linux desktops without libsecret (WSL, headless servers, minimal containers) cannot use the Secret Service. On first-time write failure, the CLI:

1. Logs a warning to stderr: `keyring unavailable on this system; storing token in ~/.config/kupe/credentials.yaml (mode 0600)`.
2. Writes to `~/.config/kupe/credentials.yaml`.
3. Sets `tokenRef: plaintext` in the context.

Users can opt into this mode explicitly via `KUPE_STORAGE=plaintext` if they want deterministic behavior across environments.

Users can opt *out* of plaintext via `KUPE_STORAGE=keyring`, which will hard-fail instead of falling back — useful for CI that doesn't want to accidentally drop a token into the image layer.

### Env var override (CI)

**Setting `KUPE_API_TOKEN` bypasses the config file entirely.** The CLI never reads the config, never touches the keyring, never writes credentials. `--tenant` (or `KUPE_TENANT`) is required.

This is the intended CI path. A workflow doing:

```yaml
env:
  KUPE_API_TOKEN: ${{ secrets.KUPE_TOKEN }}
  KUPE_TENANT: acme-corp
```

runs without any prior `kupe auth login`. `kupe auth login` itself refuses to run with a note when `KUPE_API_TOKEN` is set — it would be a no-op.

## Precedence

Every commanded that needs credentials resolves them in this order. The first non-empty source wins:

```
token:    --token flag
          → KUPE_API_TOKEN env var
          → keyring[current context]
          → credentials.yaml[current context]
          → <error: "not logged in; run `kupe auth login`">

apiUrl:   --api-url flag
          → KUPE_API_URL env var
          → contexts[current].apiUrl
          → https://api.kupe.cloud

tenant:   --tenant flag
          → KUPE_TENANT env var
          → contexts[current].tenant
          → <error: "no tenant set; pass --tenant or set KUPE_TENANT">

context:  --context flag
          → KUPE_CONTEXT env var
          → config.currentContext
          → <error: "no context; run `kupe auth login`">
```

Precedence is implemented in `internal/config/precedence.go` as a single `Resolve(flags, env, file)` function. Tests cover every layer of the chain.

## `kupe auth login` flow

### Interactive (TTY)

```
$ kupe auth login
? Tenant: acme-corp
? Paste your API token (create at https://console.kupe.cloud/settings/api-keys):
  ****************************************
✓ Logged in as billy@acme.com (admin)
  Context "acme-corp" saved, set as current.
```

Steps:

1. Prompt for tenant (hidden if `--tenant` or `KUPE_TENANT` set).
2. Prompt for token (echo suppressed via `golang.org/x/term.ReadPassword`).
3. Validate by calling `GET /api/v1/tenants/{tenant}` with the token. Stores `user` (from response) and `role` (from response) for display.
4. Write context to `config.yaml`.
5. Store token in keyring (or plaintext fallback).
6. Set `currentContext` if `--set-default` was passed, or if there are no other contexts.

### Scripted

```bash
kupe auth login \
  --tenant acme-corp \
  --token "$KUPE_BOOTSTRAP_TOKEN" \
  --context prod \
  --set-default
```

No prompts. Same validation + storage. Exits `3` on invalid token, `4` if tenant doesn't exist, `0` on success.

### Multi-context

Running `kupe auth login` again with a different tenant creates a new context (default name = tenant name; `--context` to override). The most common workflow:

```bash
kupe auth login --tenant acme-staging --context staging
kupe auth login --tenant acme-corp --context prod --set-default
kupe config use-context staging   # for a quick switch
```

## Logout

`kupe auth logout` (optionally `--context NAME` or `--all`):

1. Read `tokenRef` for the target context.
2. Delete the keyring entry or remove the credentials file entry.
3. Mark the context's `tokenRef` as empty in the config file (keeps tenant/apiUrl for easy re-login).

The context itself is preserved. Use `kupe config delete-context` to remove it entirely.

## Exec-plugin contract (for kubectl)

When you run `kupe cluster kubeconfig NAME --exec --merge`, the emitted kubeconfig looks like:

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: kupe-acme-corp-prod
    cluster:
      server: https://api.prod.acme.kupe.cloud
      certificate-authority-data: LS0tLS1CRUdJTi...
users:
  - name: kupe-acme-corp-prod
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: kupe
        args:
          - auth
          - get-token
          - --context=prod
        interactiveMode: IfAvailable
        provideClusterInfo: false
contexts:
  - name: kupe-acme-corp-prod
    context:
      cluster: kupe-acme-corp-prod
      user: kupe-acme-corp-prod
current-context: kupe-acme-corp-prod
```

When `kubectl` makes an API call, it executes `kupe auth get-token --context=prod`. The CLI:

1. Resolves the `prod` context from config.
2. Loads the token (keyring > plaintext > env — same precedence as any other command).
3. Writes an `ExecCredential` JSON to stdout:

   ```json
   {
     "apiVersion": "client.authentication.k8s.io/v1",
     "kind": "ExecCredential",
     "status": {
       "token": "kupe_8f2a_...",
       "expirationTimestamp": "2026-04-20T15:02:11Z"
     }
   }
   ```

4. Exits `0`. Any error → exit non-zero, `ExecCredential`-free stderr; `kubectl` surfaces the error to the user.

`expirationTimestamp` is the API key's `expiresAt` if set, otherwise omitted. `kubectl` caches the credential in-memory for the duration.

This pattern lets the kubeconfig be committed to a team repo (no secrets in the YAML) and rotated by simply running `kupe auth login` again with a new token.

### Why the exec plugin is gated behind `--exec`

Exec-plugin kubeconfigs require `kupe` to be on the target machine's `$PATH`. That's fine for developers but wrong for throwaway kubeconfigs dropped into a container for a single `kubectl apply`. The default (`--exec` off) embeds the bearer token directly — works everywhere, but the kubeconfig becomes a secret.

When OIDC device flow lands (Phase 1.5), `--exec` becomes the default for long-lived local use because it lets the CLI refresh expired tokens transparently.

## Rotation

API keys don't rotate automatically. To rotate:

```bash
# Create a new key (console or CLI)
kupe apikey create --name prod-rotated --role admin
# → writes kupe_... to stdout

# Store the new key
kupe config set-context prod --token kupe_...

# Revoke the old key (find its ID in the console or via list)
kupe apikey delete 8f2a3e7c-...
```

No auto-reminder on expiry in v1. Server logs `lastUsedAt` on the key and the console shows it — operators are expected to use the console for auditing.

## Security considerations

- **Config file never contains tokens** — redaction is enforced in the writer, not just the reader.
- **`-v` debug output never prints `Authorization` headers** — the HTTP middleware strips them before logging.
- **Error messages never echo the token** — only the key ID.
- **Shell history**: `kupe auth login --token "$KUPE_TOKEN"` with the token literal on the command line will be captured by shell history on most systems. Documented in the README quickstart as "use env vars, not literal tokens".
- **File mode audit**: `kupe config view -v` prints `~/.config/kupe/` directory and file modes, making it easy to spot a misconfigured `0644`.

## Open questions

Tracked separately from the implementation plan:

1. **OIDC device flow** — pending Authentik exposing `/device/code` and `/token` with `urn:ietf:params:oauth:grant-type:device_code`. When ready, `kupe auth login --method oidc` is added; `--exec` kubeconfig becomes the default to take advantage of refresh.
2. **Short-lived machine tokens via OIDC client credentials** — useful for GitHub Actions OIDC federation. Deferred.
3. **Audit log surfacing** — a `kupe auth events` subcommand that shows recent logins for the current tenant (via `GET /api/v1/tenants/{tenant}/events`, not yet in kupe-api). Deferred.
