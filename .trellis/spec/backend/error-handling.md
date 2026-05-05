# Error Handling

> How errors are handled in this project.

---

## Overview
Errors are classified close to the layer that knows the failure semantics, then mapped once at the HTTP boundary. The proxy pipeline prefers typed or sentinel errors over string matching so routing, balancing, execution, and protocol layers can stay decoupled.

---

## Error Types

- Sentinel errors:
  - `internal/protocol/openai.ErrInvalidRequest`
  - `internal/router.ErrNoRoute`
  - `internal/balancer.ErrGroupNotFound`
  - `internal/balancer.ErrNoHealthyNodes`
  - `internal/executor.ErrRelayNotImplemented`
- Structured errors:
  - `internal/executor.UpstreamError` for upstream HTTP failures
  - `internal/executor.StreamError` for streaming failures with `Started` state

Use `errors.Is` for sentinel behavior and `errors.As` when callers need upstream status/body details.

---

## Error Handling Patterns

### Scenario: Local Proxy Request Failure Mapping

#### 1. Scope / Trigger
- Trigger: Any change that affects request validation, route selection, load balancing, upstream execution, SSE streaming, or HTTP error responses.

#### 2. Signatures
- `internal/proxy.(*Service).ExecuteChat(context.Context, canonical.ChatRequest) (*canonical.ChatResponse, error)`
- `internal/proxy.(*Service).StreamChat(context.Context, canonical.ChatRequest, func(canonical.StreamChunk) error) error`
- `internal/server.writeMappedError(http.ResponseWriter, error)`
- `internal/proxy.HTTPStatus(error) int`

#### 3. Contracts
- Protocol parsing should return `ErrInvalidRequest`-wrapped errors with field-specific detail.
- Router/balancer/executor layers should return typed errors and let callers decide the HTTP mapping.
- `internal/proxy.Service` records observation metadata for both success and failure; it does not suppress the original error.
- Retry decisions are driven by `executor.IsRetryable(err)` and must stay within the selected provider group.
- Streaming failures after bytes have already been sent must not be converted into a second JSON error body.

#### 4. Validation & Error Matrix
- Malformed or incomplete OpenAI request -> HTTP `400`
- No route for requested model -> HTTP `404`
- Relay route selected in Phase 1 -> HTTP `501`
- No healthy nodes remain in provider group -> HTTP `503`
- Context deadline exceeded -> HTTP `504`
- Upstream non-2xx response -> preserve upstream status/body via `UpstreamError`
- Non-retryable stream failure before first chunk -> mapped HTTP error response
- Non-retryable stream failure after first chunk -> stop stream and surface failure through logs/observability only

#### 5. Good / Base / Bad Cases
- Good: executor returns `*UpstreamError`, server writes the upstream body/status unchanged, and proxy still records failure metadata.
- Base: handler receives `ErrInvalidRequest` and returns the standard JSON error envelope.
- Bad: convert all failures to `500` or stringify errors early so retry logic and HTTP mapping lose context.

#### 6. Tests Required
- Handler tests for `400`, `404`, `501`, upstream passthrough, and streaming behavior.
- Service tests for retries staying inside the selected group and for relay-mode refusal.
- Executor tests when retryability or stream-start behavior changes.

#### 7. Wrong vs Correct
##### Wrong
```go
if err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
    return
}
```

##### Correct
```go
if err != nil {
    writeMappedError(w, err)
    return
}
```

Why: one boundary-owned mapper keeps protocol responses stable while preserving internal error detail for retry logic and observability.

---

## API Error Responses

- Default local API error shape:

```json
{
  "error": {
    "message": "human-readable detail",
    "type": "zenhub_error"
  }
}
```

- If the upstream already returned a non-2xx response body, pass that body and status through instead of wrapping it again.

---

## Common Mistakes

- Returning plain `500` responses from handlers instead of mapping typed errors.
- Treating relay fallback as an error-recovery path inside Phase 1 direct execution.
- Retrying stream failures after bytes have already reached the client.
- Replacing typed errors with formatted strings too early, which breaks `errors.Is` / `errors.As`.
