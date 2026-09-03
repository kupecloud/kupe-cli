---
title: "Authentication & Config"
description: "Token lifecycle, config file schema, keyring storage, precedence, and exec-plugin contract for the kupe CLI"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 4
---

The CLI authenticates against `kupe-api` using bearer credentials scoped to a tenant. This doc specifies how credentials are stored, resolved, rotated, and surfaced to `kubectl` via the exec-plugin contract.

## Credential model

The default login path is OIDC. `kupe auth login --method oidc` runs an OAuth 2.0 Device Authorization Grant ([RFC 8628](https://datatracker.ietf.org/doc/html/rfc8628)) against the Authentik `kupe-cli` public client. The CLI stores the resulting access, refresh, and ID token set, then refreshes it transparently on later commands.

The non-interactive path is an API key minted against a specific tenant. Format: `kupe_<keyID>_<secret>` (prefix `kupe_` for routing on the server side, ID for auditability, secret for auth). The API validates via constant-time HMAC-SHA256 comparison against a stored hash (see [kupe-api/internal/auth/auth.go](../../kupe-api/internal/auth/auth.go)).

Two roles exist: `admin` (read-write) and `readonly`. The role is a property of the key, not the user — a tenant member can create keys in either role as long as their own role allows it.

Both login methods store credentials through the same keyring/plaintext storage layer described below. `--method apikey` is accepted as an alias for `--method token`.

## Config file

### Location

| OS | Default path | Override |
|----|--------------|----------|
| macOS | `$XDG_CONFIG_HOME/kupe/config.yaml` or `~/.config/kupe/config.yaml` | `$KUPE_CONFIG` or `--config` |
| Linux | `$XDG_CONFIG_HOME/kupe/config.yaml` or `~/.config/kupe/config.yaml` | `$KUPE_CONFIG` or `--config` |
| Windows | `%AppData%\kupe\config.yaml` | `$KUPE_CONFIG` or `--config` |

For consistency with kubectl and gh, macOS and Linux users without `XDG_CONFIG_HOME` set will see `~/.config/kupe/config.yaml`.

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
    authMethod: oidc           # "oidc" | "apikey"
  - name: staging
    apiUrl: https://api.staging.kupe.cloud
    tenant: acme-staging
    tokenRef: keyring
    user: billy@acme.com
    authMethod: oidc
    oidcBaseUrl: https://auth.staging.kupe.cloud
    oidcClientId: kupe-cli
    signupUrl: https://signup.staging.kupe.cloud   # signup service for "kupe user delete"; default signup.kupe.cloud
preferences:
  output: table                # table | wide | json | yaml | name
  color: auto                  # auto | always | never
  wait: true
  waitTimeout: 30m
```

**Tokens are never written into this file.** `tokenRef` is a pointer to where the token actually lives (the OS keyring, or the plaintext-fallback credentials file). See [Token storage](#token-storage).

`preferences.output` is applied as the default for commands with an `-o` flag. `preferences.color`, `preferences.wait`, and `preferences.waitTimeout` are accepted by `kupe config get/set` but are not currently wired into command defaults.

The loader validates `apiVersion` and `kind`, then normalizes omitted identity fields. Unknown YAML fields are ignored by `yaml.v3`; use `kupe config view` to see the effective schema the CLI preserves when it writes the file.

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

Service key: `cloud.kupe.cli`. Account key: the context name (e.g., `prod`). A single binary invocation touches exactly one keyring entry.

On login, the CLI writes to the keyring and sets `tokenRef: keyring` in the config. On logout, it deletes the keyring entry.

### Plaintext fallback

Linux desktops without libsecret (WSL, headless servers, minimal containers) cannot use the Secret Service. Very large OIDC token sets can also exceed some keyring limits. On write failure that the manager classifies as unavailable or too large, the CLI:

1. Logs a warning to stderr that the OS keyring rejected the credential and the CLI fell back to plaintext.
2. Writes to `~/.config/kupe/credentials.yaml`.
3. Sets `tokenRef: plaintext` in the context.

Users can opt into this mode explicitly via `KUPE_STORAGE=plaintext` if they want deterministic behavior across environments.

Users can opt *out* of plaintext via `KUPE_STORAGE=keyring`, which will hard-fail instead of falling back — useful for CI that doesn't want to accidentally drop a token into the image layer.

### Env var override (CI)

**Setting `KUPE_API_TOKEN` bypasses keyring/plaintext token lookup.** The CLI still resolves `--api-url`, `--tenant`, and `--context` through the normal flag/env/config precedence chain. In config-free CI, set `KUPE_TENANT` as well.

This is the intended CI path. A workflow doing:

```yaml
env:
  KUPE_API_TOKEN: ${{ secrets.KUPE_TOKEN }}
  KUPE_TENANT: acme-corp
```

runs without any prior `kupe auth login`. `kupe auth login` itself refuses to run with a note when `KUPE_API_TOKEN` is set — it would be a no-op.

## Precedence

Every command that needs credentials resolves them in this order. The first non-empty source wins:

```
token:    --token flag
          → KUPE_API_TOKEN env var
          → keyring or credentials.yaml for the resolved context's tokenRef
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
          → empty (valid for direct-token commands that also provide a tenant)
```

Precedence is implemented in `internal/config/precedence.go` as a single `Resolve(flags, env, file)` function. Tests cover every layer of the chain.

## `kupe auth login` flow

### Interactive (TTY)

```
$ kupe auth login --tenant acme-corp
To finish signing in, open the following URL in any browser:

    https://auth.kupe.cloud/...

and enter this code:

    ABCD-EFGH

Waiting for approval (code expires in 15m0s)...

Logged in to tenant Acme Corp (acme-corp) as billy@acme.com.
```

Steps:

1. Prompt for tenant (hidden if `--tenant` or `KUPE_TENANT` set).
2. Start the OIDC device-code flow and print the verification URL and user code.
3. Poll Authentik until the user approves and a token set is returned.
4. Validate by calling `GET /api/v1/tenants/{tenant}` with the access token.
5. Store the OIDC token set in keyring (or plaintext fallback).
6. Write context to `config.yaml`, including `authMethod: oidc` and any non-default OIDC settings.
7. Set `currentContext` if `--set-default` was passed, or if there are no other contexts.

For API-key login, pass `--method token`; the CLI prompts for the token with echo suppressed when no `--token` flag is provided.

### Scripted

```bash
kupe auth login \
  --method token \
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

When you run `kupe cluster kubeconfig NAME --exec --merge`, the emitted kubeconfig looks like this. The `command` is usually the absolute path to the current `kupe` binary; `kupe` is shown here for readability.

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: kupe-acme-corp-prod
    cluster:
      server: https://prod.acme.clusters.kupe.cloud
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
2. Loads the token (`--token`/`KUPE_API_TOKEN` if present, otherwise the resolved context's keyring/plaintext `tokenRef`).
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

`expirationTimestamp` is set when the resolved credential is an OIDC access token with a known expiry. API-key contexts omit it, so `kubectl` may invoke the exec plugin more often; that path is a cheap keyring/plaintext read.

This pattern lets the kubeconfig be committed to a team repo (no secrets in the YAML) and rotated by simply running `kupe auth login` again with a new token.

### Why the exec plugin is gated behind `--exec`

Exec-plugin kubeconfigs require the `kupe` binary path embedded in the kubeconfig to exist on the target machine. That's fine for developers but wrong for throwaway kubeconfigs dropped into a container for a single `kubectl apply`. The default (`--exec` off) embeds the bearer token directly — works everywhere, but the kubeconfig becomes a secret.

For OIDC contexts, `--exec` is recommended for long-lived local use because the exec plugin shells back into `kupe auth get-token`, letting the CLI refresh expired access tokens transparently. For API-key contexts, the embedded-token form is fine for short-lived kubeconfigs because there is no refresh token to rotate.

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

## Open questions

Tracked separately from the implementation plan:

1. **Short-lived machine tokens via OIDC client credentials** — useful for GitHub Actions OIDC federation. Deferred.
2. **Audit log surfacing** — a `kupe auth events` subcommand that shows recent logins for the current tenant (via `GET /api/v1/tenants/{tenant}/events`, not yet in kupe-api). Deferred.
