---
title: "API Client"
description: "HTTP client architecture, retry policy, ETag handling, error classification, and the relationship to terraform-provider-kupe"
owner: platform-team
lastReviewed: "2026-04-20"
sidebar:
  order: 5
---

The CLI ships with a hand-written Go client for `kupe-api`. This doc explains why it's hand-written, how it's derived from the terraform-provider-kupe client, and how extensions (retry, ETag RMW, typed errors) are layered on top.

## Why hand-written, not generated

We evaluated three options:

1. **Generate from [kupe-api/api/swagger.json](../../kupe-api/api/swagger.json)** via `oapi-codegen` or `swagger-codegen`.
2. **Hand-write** — copy the existing working client from `terraform-provider-kupe` and extend it.
3. **Consume the `terraform-provider-kupe/internal/client/` package directly** — impossible because it's under `internal/`.

Hand-written wins on three points:

- **Ergonomics.** The tf provider's client already has the shape commands want — `ListClusters(ctx) ([]Cluster, error)` — whereas code generators produce `ListClustersWithResponse(ctx, params, ...) (*ListClustersResponse, error)` with layer after layer of boxed types. The CLI would spend more code unwrapping generated types than we save by not typing them.
- **Swagger 2.0 tooling is weaker than OpenAPI 3.** kupe-api emits Swagger 2.0 today (from `swag` on Go comments). Swagger 2.0 codegen output in Go is notably worse than OpenAPI 3 output. Converting the spec first adds a pipeline step for little benefit.
- **The spec is already proven and tested.** `terraform-provider-kupe/internal/client/*` has real tests (`client_test.go`, `cluster_test.go`, `apikey_test.go`, etc.). Throwing that away to regenerate is net-negative.

The swagger spec is used as a **coverage checklist**, not as codegen input. [commands.md](./commands.md) has a table mapping every endpoint in the spec to either a CLI command or "Phase 2 / out of scope".

## Strategy: lift, then extend

### Step 1 — copy

Copy these files from [terraform-provider-kupe/internal/client/](../../terraform-provider-kupe/internal/client/) into `kupe-cli/internal/client/`:

| File | Contents |
|------|----------|
| `client.go` | Base HTTP plumbing: `Client` struct, `request()`, `requestWithETag()`, `APIError`, `IsNotFound`, `IsConflict`. |
| `cluster.go` | `Cluster`, `ClusterResource`, `ClusterStatus`, `CreateClusterRequest`, `PatchClusterRequest`; `ListClusters`, `GetCluster`, `CreateCluster`, `UpdateCluster`, `DeleteCluster`. |
| `apikey.go` | APIKey types + CRUD. |
| `tenant.go` | `GetTenant` (used by `whoami`). |
| Corresponding `*_test.go` | Tests that ride along — they validate the client still works after extensions. |

`member.go`, `secret.go`, and `alertmanager.go` can be lifted too but are Phase 2 — ignore until those commands are needed.

### Step 2 — rebrand

Change `User-Agent` from `terraform-provider-kupe` to:

```go
fmt.Sprintf("kupe-cli/%s (%s/%s) go/%s",
    build.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
```

`build.Version` comes from `internal/build/info.go`, set by goreleaser ldflags (see [distribution.md](./distribution.md)).

### Step 3 — add extensions

Five concerns the tf provider's client doesn't handle that the CLI needs:

1. Retry/backoff on transient failures.
2. `Retry-After` on 429.
3. ETag read-modify-write helper for `update` commands.
4. Typed error helpers (mirror kupe-api's `errors.go` classification).
5. Request-ID propagation into error messages.

Each is covered below.

## Base client

Unchanged from the tf provider — reproduced here for reference (full file at [terraform-provider-kupe/internal/client/client.go](../../terraform-provider-kupe/internal/client/client.go)):

```go
type Client struct {
    baseURL    string
    tenant     string
    token      string
    httpClient *http.Client
}

func New(baseURL, tenant, token string) *Client {
    return &Client{
        baseURL: baseURL,
        tenant:  tenant,
        token:   token,
        httpClient: &http.Client{Timeout: 30 * time.Second},
    }
}
```

`request()` marshals the body, sets headers (`Authorization`, `Accept: application/json`, `Content-Type`, `User-Agent`, optional `If-Match`), sends, then unmarshals the response. Returns the response `ETag` alongside any error.

Errors non-2xx → `*APIError{StatusCode, Message}`. JSON error bodies are decoded as `{"error": "..."}` (matches `kupe-api`'s `response.go`).

## Retry policy

Implemented in a new `internal/client/retry.go`:

```go
type retryPolicy struct {
    maxAttempts     int           // 3
    initialBackoff  time.Duration // 100ms
    maxBackoff      time.Duration // 1600ms
    backoffFactor   float64       // 2
}
```

Retries on:

- Network errors (any `net.Error` with `Timeout()` or `Temporary()`).
- HTTP `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`.

Does **not** retry on:

- `4xx` errors (including `429` — handled separately, see below).
- `500 Internal Server Error` — we'd rather fail loudly and surface the `X-Request-Id` than mask a real bug.
- `context.Canceled` / `context.DeadlineExceeded`.

Jitter: equal jitter (`sleep = rand(backoff/2, backoff)`) to avoid thundering herd on a shared `kupe-api`.

### Not retried: POST and PATCH with bodies

Create/update operations are **not** retried automatically, even on `503`. A successful-looking retry could produce a second resource or a duplicate `409` error if the first attempt landed. The rule is:

- GET, DELETE, HEAD → retry per policy.
- POST, PATCH, PUT → do not retry; surface the error.

This matches Kubernetes client-go behavior and the gh/fly convention.

## Rate limiting (429)

Separate from the generic retry. On `429 Too Many Requests`:

1. Parse `Retry-After` header. Accept both forms:
   - Seconds (integer): `Retry-After: 10`
   - HTTP-date: `Retry-After: Wed, 21 Oct 2026 07:28:00 GMT`
2. Cap the wait at 30 seconds (prevents a hostile server from hanging the CLI).
3. If `Retry-After` is missing, wait 5 seconds.
4. Retry **once** regardless of method. If the second attempt also returns `429`, surface the error.

With `-v`, prints:

```
[verbose] rate limited by api.kupe.cloud; retrying in 10s (request-id: 7a3b9e41-...)
```

## ETag read-modify-write

`kupe-api` supports optimistic locking via `ETag` (on responses) and `If-Match` (on PATCH requests). `update` commands need to do:

1. GET the current resource → receive `ETag`.
2. Apply the mutation (set new version, new resources, etc.).
3. PATCH with `If-Match: <ETag>`.
4. On 412, reload and retry once.

A helper in `internal/client/cluster.go`:

```go
type ClusterMutator func(*Cluster) *PatchClusterRequest

func (c *Client) UpdateClusterRMW(ctx context.Context, name string, mutate ClusterMutator) (*Cluster, error) {
    for attempt := 0; attempt < 2; attempt++ {
        current, etag, err := c.GetCluster(ctx, name)
        if err != nil { return nil, err }

        patch := mutate(current)
        updated, _, err := c.UpdateCluster(ctx, name, etag, *patch)
        if err == nil { return updated, nil }
        if !IsPreconditionFailed(err) { return nil, err }
        // 412 → someone else updated; loop once
    }
    return nil, errors.New("update contention too high; try again")
}
```

Commands call `UpdateClusterRMW`. The explicit form (`--if-match <ETAG>`) bypasses the RMW loop and hands the ETag through directly — advanced users who want to detect drift explicitly.

`--force` fetches current, skips the `If-Match` header, and sends the patch unconditionally. Used sparingly — documented with a warning in `commands.md`.

## Error classification

`kupe-api` emits classified errors with stable HTTP status codes (see [kupe-api/internal/errors/errors.go](../../kupe-api/internal/errors/errors.go)):

| kupe-api type | HTTP | CLI helper | CLI exit code |
|---------------|------|------------|---------------|
| `ErrorTypeUnauthorized` | 401 | `IsUnauthorized(err)` | 3 |
| `ErrorTypeForbidden` | 403 | `IsForbidden(err)` | 3 |
| `ErrorTypeNotFound` | 404 | `IsNotFound(err)` | 4 |
| `ErrorTypeValidation` | 400 | `IsValidation(err)` | 2 |
| `ErrorTypeConflict` | 409 | `IsConflict(err)` | 5 |
| `ErrorTypePreconditionFailed` | 412 | `IsPreconditionFailed(err)` | 5 |
| `ErrorTypeRateLimited` | 429 | `IsRateLimited(err)` | 6 |
| `ErrorTypeUnavailable` | 503 | `IsUnavailable(err)` | 7 |
| `ErrorTypeInternal` | 500 | — | 1 |

Helpers in `internal/client/errors.go` wrap the tf provider's existing `APIError`:

```go
func IsUnauthorized(err error) bool {
    var apiErr *APIError
    return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}
// ... IsForbidden, IsValidation, IsPreconditionFailed, IsRateLimited, IsUnavailable
```

`IsNotFound` and `IsConflict` already exist in the tf provider — re-exported.

## Request ID propagation

`kupe-api` emits `X-Request-Id` on every response (likely to land; verify against the API's current middleware stack). When present, the client attaches it to errors:

```go
type APIError struct {
    StatusCode int
    Message    string
    RequestID  string  // new
}

func (e *APIError) Error() string {
    if e.RequestID != "" {
        return fmt.Sprintf("kupe api: %d %s (request-id: %s)", e.StatusCode, e.Message, e.RequestID)
    }
    return fmt.Sprintf("kupe api: %d %s", e.StatusCode, e.Message)
}
```

The CLI's error printer (see [design.md](./design.md)) picks up `RequestID` and displays it as a hint line, so support tickets can reference it.

## Timeouts

| Operation | Timeout | Configurable |
|-----------|---------|--------------|
| Single HTTP request | 30s | Hardcoded in `Client` constructor; matches tf provider. |
| Command-level context | Command-specific | `--wait-timeout` for long-ops, no explicit flag for one-shots (Cobra's `--timeout` deferred to Phase 2). |

A long-running command (`cluster create --wait`) applies `--wait-timeout` via `context.WithTimeout` on the top-level context. Individual polls inside the waiter each get 30s. If `kupe-api` itself is slow, a poll can fail and be retried on the next interval.

## Observability hooks

The client exposes two hooks for testing and verbose mode:

```go
type Transport struct {
    Base      http.RoundTripper  // defaults to http.DefaultTransport
    OnRequest func(*http.Request)
    OnResponse func(*http.Response, time.Duration)
}
```

- `-v` attaches hooks that log method, path, status, duration, and request-id (no headers, no body).
- Tests attach hooks that assert on headers or inject failures.

No bodies are ever logged. `Authorization: Bearer ...` is redacted if any code path were to log headers (defense in depth).

## Phase 2: extract to a shared module

Once the CLI and terraform provider have both stabilized, extract `internal/client/` into a new repo `github.com/kupecloud/kupe-go-client` and import it from both consumers. Benefits:

- Single source of truth for API types — no drift between the CLI's `Cluster` and the tf provider's `Cluster`.
- Shared tests.
- External consumers can use it for integrations (Crossplane provider, third-party tooling).

Constraints that must be preserved during extraction:

- No dependency on Cobra, cli-runtime, or anything CLI-specific. The client is a pure HTTP library.
- No dependency on Terraform Plugin Framework, either.
- Public API: `Client`, resource types, `APIError`, `Is*` helpers, `UpdateClusterRMW` (or its generalized variant).

Timing: target around the v1.0 CLI release. Until then, the two consumers each own their copy and can drift if the API changes in a way that affects only one.

## Reference

- [terraform-provider-kupe/internal/client/client.go](../../terraform-provider-kupe/internal/client/client.go) — base HTTP client to lift.
- [terraform-provider-kupe/internal/client/cluster.go](../../terraform-provider-kupe/internal/client/cluster.go) — reference cluster resource shape.
- [kupe-api/internal/errors/errors.go](../../kupe-api/internal/errors/errors.go) — error taxonomy.
- [kupe-api/api/swagger.json](../../kupe-api/api/swagger.json) — authoritative endpoint list.
- [kupe-api/internal/server/handler_cluster.go](../../kupe-api/internal/server/handler_cluster.go) — request/response shapes for cluster endpoints.
- [kupe-control-operator/api/v1alpha1/managedcluster_types.go](../../kupe-control-operator/api/v1alpha1/managedcluster_types.go) — field validation regexes (CPU/memory/storage quantity formats).
