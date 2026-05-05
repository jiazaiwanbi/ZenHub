# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

These rules capture backend contracts that are easy to drift during fast iteration and expensive to rediscover during review.

---

## Forbidden Patterns

### Scenario: Canonical Request Transformation

#### 1. Scope / Trigger
- Trigger: Cross-layer request handling that parses protocol input and later serializes an upstream provider payload.

#### 2. Signatures
- `internal/core/protocol/openai.ParseChatCompletion(io.Reader) (canonical.ChatRequest, error)`
- `internal/core/transformer.OpenAIChatRequest(req canonical.ChatRequest, upstreamModel string, forceStream *bool) ([]byte, error)`

#### 3. Contracts
- Known OpenAI request fields: `model`, `messages`, `tools`, `temperature`, `top_p`, `max_tokens`, `stream`, `metadata`
- Those known fields must be serialized from `canonical.ChatRequest` and `canonical.Message`, not copied wholesale from a raw JSON field map.
- `upstreamModel` may override `req.Model` at the provider boundary.
- `forceStream` may override `req.Stream` at the provider boundary.
- Raw field maps are only for preserving unknown extension fields or explicit raw values when the canonical struct does not carry them directly.

#### 4. Validation & Error Matrix
- Missing `model` at protocol ingress -> `ErrInvalidRequest`
- Empty `messages` at protocol ingress -> `ErrInvalidRequest`
- Malformed message JSON at protocol ingress -> `ErrInvalidRequest`
- Marshal failure while rebuilding provider payload -> return transformer error to caller

#### 5. Good / Base / Bad Cases
- Good: Parse OpenAI JSON once, route with `canonical.ChatRequest`, then rebuild upstream JSON from canonical fields plus extension passthrough.
- Base: Canonical request created internally without any `RawFields` still produces a valid upstream payload.
- Bad: Clone `req.RawFields`, patch `model`, and forward the rest unchanged.

#### 6. Tests Required
- Unit test: canonical-only request serializes `model`, `messages`, and `stream`
- Unit test: stale `RawFields` do not override canonical message/model values
- Unit test: unknown extension fields are preserved during serialization

#### 7. Wrong vs Correct
##### Wrong
```go
fields := cloneRawMap(req.RawFields)
fields["model"] = encodedModel
return json.Marshal(fields)
```

##### Correct
```go
fields := cloneRawMap(req.RawExtensions)
fields["model"] = encodedModel
fields["messages"] = encodedMessages
fields["stream"] = encodedStream
return json.Marshal(fields)
```

Why: forwarding the raw protocol map makes route-time canonicalization meaningless and breaks future provider/protocol expansion.

---

## Required Patterns

- Rebuild provider payloads from canonical structs for all known fields.
- Keep unknown protocol/provider extensions in explicit raw-extension buckets instead of mixing them with canonical data.
- Add regression tests whenever a boundary bug is fixed in protocol, transformer, router, balancer, or executor layers.

---

## Testing Requirements

- Add unit coverage for any new canonical parsing or transformer behavior.
- Add handler or service coverage when the bug spans routing, balancing, execution, or streaming behavior.
- Run `gofmt`, `go vet ./...`, and `go test ./...` before closing backend review work.

---

## Code Review Checklist

- Does the code use canonical structs as the source of truth across layer boundaries?
- Are retries, route decisions, and provider execution isolated to their own packages?
- Do tests prove both the happy path and the regression that motivated the change?
