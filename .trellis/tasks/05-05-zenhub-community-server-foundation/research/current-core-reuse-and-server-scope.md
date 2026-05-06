# Research: Current Core Reuse And Community Server Scope

## Current reusable shared code

The repo now has a meaningful shared core surface:

- `internal/core/canonical`
- `internal/core/protocol/openai`
- `internal/core/router`
- `internal/core/balancer`
- `internal/core/transformer`
- `internal/core/proxy`
- `internal/core/observability`
- `internal/core/executor`

These packages can support future server relay logic and models exposure
without reusing client-local runtime or GUI code.

## Current client-only code

The following packages are product-specific client code and should not be
treated as shared server infrastructure:

- `internal/client/app`
- `internal/client/config`
- `internal/client/gui`
- `internal/client/localhostapi`

## Community server API scope from requirements

The open-source server API draft requires:

- `POST /api/v1/auth/login`
- `POST /api/v1/sync/pull`
- `POST /api/v1/sync/push`
- `GET /api/v1/sync/status`
- `POST /api/v1/relay/chat/completions`
- `GET /api/v1/models`

The product scope is explicitly single-user and community/open-source.

## Practical first-slice interpretation

The smallest meaningful first server slice should:

- create a dedicated community server binary
- establish server-only packages
- implement a single-user auth boundary
- define a snapshot/sync contract
- expose server-side models and relay APIs

This is enough to prove the new `server` product line exists without dragging
in hosted-only concerns such as refresh tokens, multi-user isolation, catalog,
or billing.

## Storage note

The requirements say server-side storage should ultimately use MySQL.

For implementation sequencing, the team must choose between:

1. concrete MySQL-backed storage in this first slice
2. storage interface plus a simpler first implementation, with MySQL next

Either way, the package layout should reserve `storage/mysql` as the long-term
server direction and avoid reusing client-local config/state code as a fake
server database layer.
