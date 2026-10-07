# file-uploader

A Go mono repo of independent microservices for uploading files to S3. `api-service` is the only publicly exposed service; it forwards uploads to `file-service`, which stores them in S3, and checks logins against `user-service`, which owns user accounts in PostgreSQL. `file-processing-service` is still at skeleton stage.

## Requirements

- Go 1.26+
- Docker (required for `make test-e2e` and `file-service`'s mTLS integration tests; optional otherwise, e.g. for building images or `make run`)
- AWS credentials with access to the target S3 bucket (for `file-service`)

## Services

| Service                   | Exposed | Purpose                                        |
| -------------------------- | ------- | ----------------------------------------------- |
| `api-service`              | Public  | Gateway; validates and forwards file uploads; issues JWTs on login after checking credentials with `user-service` (see [Authentication](#authentication)) |
| `file-service`             | Internal| Streams uploaded files to S3                    |
| `user-service`             | Internal| User accounts and credential checks over gRPC (mTLS), backed by PostgreSQL (see [user-service](#user-service)) |
| `file-processing-service`  | Internal| File processing (skeleton)                      |

Each service exposes:

- `GET /health` — health check, returns `{"status": "ok"}` (`api-service`, `file-service`, `user-service`)
- `POST /api/v1/files/upload` — uploads a single file (`api-service`, `file-service`); requires a valid JWT
- `POST /login` — authenticates a user and returns a JWT (`api-service` only, see [Authentication](#authentication))
- `POST /register` — creates a user account (`api-service` only, public; see [Authentication](#authentication))
- gRPC `users.Users` — `GetByUsername`, `Create` and `Authenticate` (`user-service` only, on its mTLS port; see [user-service](#user-service))

### File upload

Uploads are streamed rather than buffered, with metadata carried in headers:

| Header                 | Payload field | Notes                                  |
| ----------------------- | -------------- | --------------------------------------- |
| `Content-Disposition`   | `filename`     | Must end in `.csv`                      |
| `Content-Length`        | `size`         | Max 10Mb                                 |
| `Content-Type`          | `contentType`  | Must be `text/csv`                       |
| `X-Checksum-Sha256`     | `checksum`     | Hex-encoded SHA-256 digest of the body   |

`api-service` validates and caps the body at 10Mb before forwarding it to `file-service`, which verifies the checksum and uploads to S3 via `PutObject`, passing the checksum through for S3-side integrity verification.

## Authentication

`api-service` exposes `POST /login`, which accepts a JSON body `{"username": "...", "password": "..."}` and returns `{"token": "<jwt>"}` on success. Login checks the credentials by calling `user-service`'s `Authenticate` over gRPC with mutual TLS, forwarding the request's `x-request-id`. A wrong password and an unknown user both come back as `401 invalid_credentials`, and an unreachable `user-service` as `503 unavailable`. `api-service` still signs the token itself rather than `user-service`; moving token issuance there is a possible follow-up. `POST /register` creates an account: it takes `{"username": "...", "email": "...", "password": "..."}` and calls `user-service`'s `Create`, returning `201` with `{"id": ..., "username": "...", "email": "..."}`. It is public and unversioned like `/login`, and runs the same middleware chain. The body is validated against the same rules as `user-service` (lowercase username, 8–72 printable ASCII password), and `user-service` additionally rejects reserved usernames and passwords missing an uppercase letter, a lowercase letter, a digit or a special character (`400 invalid_input`, with the reason in the message). A taken username or email gives `409 user_exists` (one response for both, so it doesn't say which field clashed), and an unreachable `user-service` gives `503`. Registration is not rate-limited yet, and every attempt costs a bcrypt hash, so put a limit in front of it before exposing it publicly.

The returned JWT is HS256-signed using the `JWT_SECRET` environment variable, with a 15-minute expiry (`sub` set to the username, plus standard `iat`/`exp` claims). `api-service` **requires `JWT_SECRET` to be set** and will refuse to start without it.

`POST /api/v1/files/upload` requires a valid token via `Authorization: Bearer <token>`, enforced by `api-service/internal/middleware.JWT`. `POST /login` and `GET /health` are intentionally exempt: `/login` is how a client obtains a token in the first place, and `/health` is probed by infra (e.g. a kubelet) that never presents one.

## user-service

`user-service` serves a gRPC API (`GetByUsername`, `Create`, `Authenticate`) on its mTLS port (`GRPC_PORT`, default `8443`) and a plain-HTTP `/health` on `HEALTH_PORT` (default `8080`). The API is design-first with goa, like the other services; the result type never includes the password hash.

Code is layered handler → service → repository:

- `internal/api` — the goa handler: converts payloads and results and maps domain errors to goa errors, plus the routes (`GRPCServer`, `HealthRoutes`).
- `internal/service/users` — the business logic: reserved usernames (`admin`, `root`, `system`, `support`), password strength, bcrypt hashing (default cost) and a timing-safe `Authenticate` (an unknown user still costs one bcrypt comparison). It defines the `Repository` interface and the domain errors.
- `internal/repository/users` — implements that interface over the sqlc-generated queries in `internal/repository/gen`, mapping `pgx.ErrNoRows` and unique violations to domain errors.

Validation rules, enforced at the API boundary by the goa design:

- Username: 3–50 characters, `^[a-z][a-z0-9_]*$` (lowercase only, starts with a letter).
- Password (on `Create`): 8–72 printable ASCII characters (no spaces or emoji, since bcrypt truncates at 72 bytes), and the service additionally requires an uppercase letter, a lowercase letter, a digit and a special character. `Authenticate` only checks that the password is 8–72 characters, so a later change to the creation rules can't lock out existing users.

The database is PostgreSQL; the schema lives in `user-service/db/migrations` (goose) and queries in `user-service/db/queries` (sqlc). The username must be lowercase (`CHECK` constraint) and unique, and emails are unique case-insensitively (an index on `lower(email)`).

Configuration (env var or flag): `DATABASE_URL` (env only, so the password never appears on the command line), `HEALTH_PORT`, `GRPC_PORT`, `TLS_CERT_FILE`, `TLS_KEY_FILE`, `TLS_CA_CERT_FILE`, `DEBUG`. See `user-service/.env.example`. `DATABASE_URL` must include a user, password, host, database name and `sslmode=verify-full`, and the service refuses to start otherwise.

## Internal service-to-service security

`api-service`'s calls to `file-service`'s upload endpoint are protected with mutual TLS: each service presents a certificate signed by a shared local development CA (generated by `make certs`), and `file-service` rejects any request to its internal files port that doesn't present a valid client certificate. `file-service`'s `/health` endpoint stays on its own plain-HTTP port, since it's probed by infra (e.g. kubelet) without a client certificate. See `adrs/0001-mtls-between-internal-services.md` for the full decision record.

`api-service`'s gRPC calls to `user-service` follow the same pattern: `user-service` serves gRPC on an mTLS port that requires a client certificate signed by the shared CA, with `/health` on a separate plain-HTTP port.

`user-service`'s connection to PostgreSQL is TLS-only. The client side insists on `sslmode=verify-full` (it validates the server certificate and hostname), and the local database is additionally configured to refuse any non-TLS connection (`user-service/db/pg_hba.local.conf` has only a `hostssl` rule).

## Running locally

```sh
make certs
make run
```

`make certs` generates a local development CA and leaf certificates for `api-service`, `file-service`, `user-service` and the `user-db` Postgres under `certs/` (gitignored). Run it once before the first `make run`, and again if `certs/` is ever deleted — the services will refuse to start without a valid cert/key/CA on disk.

`make run` builds and starts `api-service`, `file-service`, `user-service` and its PostgreSQL (`user-db`) via `docker-compose.local.yml`; a one-shot `user-db-migrate` container applies the goose migrations before `user-service` starts. `file-service` mounts your local `~/.aws` credentials read-only, so make sure `AWS_PROFILE` is configured on the host. Copy each service's `.env.example` to `.env` and fill in the required values (`S3_BUCKET_NAME`, `AWS_PROFILE`, `AWS_REGION`, `JWT_SECRET`) — the root `.env` feeds `docker-compose.local.yml` directly, and `api-service` will refuse to start without `JWT_SECRET` set. The local database user, password and name default to `user_service`/`user_service`/`users` and can be overridden with `USER_DB_USER`, `USER_DB_PASSWORD` and `USER_DB_NAME`.

There are no users in a fresh database, so create one with `POST /register` (for example `curl -X POST localhost:8080/register -H 'Content-Type: application/json' -d '{"username":"john_doe","email":"john.doe@example.com","password":"Correct-Horse-42"}'`) before trying `/login`. The default container ports are 8080 (HTTP) and 8443 (mTLS); `user-service`'s health port is published on host port `8082`, its gRPC port isn't published.

To run migrations by hand from `user-service/`, export `GOOSE_DRIVER`, `GOOSE_DBSTRING` and `GOOSE_MIGRATION_DIR` (see `user-service/.env.example`) and use `make migrate-up`, `migrate-down`, `migrate-status` or `migrate-create NAME=...`.

## End-to-end tests

`test/integration/` holds a Ginkgo+Gomega end-to-end suite (build-tagged `e2e`) that brings up a real `api-service` + `user-service` + `file-service` stack, plus PostgreSQL for `user-service` and a LocalStack container (S3 only), using testcontainers-go's compose module, then drives it over plain HTTP exactly like a real client would — register, login, then upload — asserting against the object actually stored in S3 rather than mocks. It only ever calls `api-service`'s public port.

```sh
make test-e2e
```

This regenerates certs (`make certs`) and runs `go test -tags=e2e ./test/integration/...`; it needs Docker and isn't part of `make`/`make all`, since it builds and starts real containers rather than running in-process. `file-service` picks up an optional `S3_ENDPOINT` env var (unused — and a no-op — in production) to point its S3 client at LocalStack instead of real AWS for this suite.

The stack also includes `user-service` and its PostgreSQL (TLS-only, with the migrations applied by a one-shot goose container), so the suite exercises the real `/register` → `/login` → upload path. `BeforeSuite` registers the test user through `POST /register` rather than seeding the database directly.

## Make targets

Run from the repo root:

| Target                          | Description                                                       |
| -------------------------------- | ------------------------------------------------------------------ |
| `make`                           | Build+test every service and `common/`                             |
| `make certs`                     | Generate local dev CA + leaf certs for mTLS between services        |
| `make run`                       | Run `api-service` + `file-service` locally via docker compose       |
| `make test-e2e`                  | Run the `test/integration/` e2e suite against a real api-service + user-service + file-service + PostgreSQL + LocalStack stack (see [End-to-end tests](#end-to-end-tests)) |
| `make all-services TARGET=<t>`   | Run `make <t>` in every service                                    |
| `make common-pkg-test`           | Vet + race-test only `common/...`                                  |

Run from a single service directory (or `make -C <service> <target>` from the root):

| Target             | Description                                                  |
| ------------------ | ------------------------------------------------------------ |
| `make`             | Run tests, check coverage and build                          |
| `make build`       | Build the binary into `./bin/`                               |
| `make test`        | Run `go fmt`, `go vet` and tests with the race detector      |
| `make test-integration` | Same as `make test`, plus any `//go:build integration` tests (tagged, same packages). For `file-service`, this also runs `make certs` first, since its mTLS integration tests need real dev certs on disk. For `user-service`, the repository tests start a real PostgreSQL container via testcontainers-go, so it needs Docker (and a longer `INTEGRATION_TEST_TIMEOUT` on a first run that has to pull the image). |
| `make coverage`    | Generate a coverage report; fails if total coverage is below 70% |
| `make clean`       | Remove build and coverage artifacts                          |
| `make generate`    | Run `goa gen` against the service's `design` package         |
| `make docker-build`| Build the Docker image                                       |
| `make docker-push` | Build and push the Docker image                              |

`user-service`'s own `Makefile` adds `migrate-status`, `migrate-up`, `migrate-down` and `migrate-create NAME=<name>` (goose, configured through `GOOSE_*` env vars), `sqlc-generate` (regenerates `internal/repository/gen` from `db/`), and `protoc-gen-go`/`protoc-gen-go-grpc` (install the protobuf plugins `make generate` needs; make sure `$(go env GOPATH)/bin` is on your `PATH`).

The image name and tag can be overridden:

```sh
make docker-push DOCKER_REPO=<user>/<project> DOCKER_IMAGE_TAG=v1.0.0
```

Note: **Docker build context is always the repo root** — always invoke `docker build`/`make docker-build` from the root, not the service directory.

## Logging

`common/logger` provides a JSON `slog` logger that you store in and read from a `context.Context`:

```go
log := logger.NewLogger(debug)
ctx := logger.WithCtx(ctx, log)

logger.FromCtx(ctx).Info("server started", "port", port)
```

`FromCtx` panics if the context has no logger, so attach one early (e.g. in `main` or in middleware). `logger.RequestMiddleware(log, mandatory bool)` always attaches a per-request logger to the context — tagged with the request ID once one has been populated on the context, without that tag otherwise. If `mandatory` is `true` and no request ID is present by the time this middleware runs, it panics instead of silently logging without one, so a route wired with `mandatory=true` must have a request-ID-populating middleware (goa's `RequestID` or `RequireRequestID`, see below) ahead of it in the chain.

## Request IDs

`common/http/middleware` propagates an `X-Request-Id` header across service calls:

- `RequireRequestID()` rejects internal-service requests that arrive without one (used by `file-service`'s files endpoint, which is never called directly by clients).
- `PanicHandler()` recovers from panics in HTTP handlers and returns a 500 instead of crashing the process.
- `common/http/client.NewDoer` returns a goa HTTP `Doer` that forwards the request ID from context onto outgoing requests, so `api-service` calling into `file-service` preserves the same ID end-to-end.

The gRPC hop to `user-service` carries the ID as `x-request-id` metadata (gRPC metadata keys are lowercase), using `common/grpc/interceptor`:

- `ClientRequestID()` — a client interceptor on `api-service`'s connection that copies the ID from the context into outgoing metadata.
- `RequestID(required)` — a server interceptor that reads the metadata value into the context, rejecting calls without it as `InvalidArgument` when `required` is true (the counterpart of `RequireRequestID()`).
- `Logger(log, mandatory)` — a server interceptor that tags the request-scoped logger with `reqID` from the context (the counterpart of `logger.RequestMiddleware`; it panics if `mandatory` is true and no ID is present, so `RequestID` must come first in the chain).
- `Recover(log)` — turns a panic in a gRPC handler into an `Internal` status and logs it.

`/login` therefore runs goa's `RequestID` middleware (and `RequestMiddleware(log, true)`), since it has to forward an ID to `user-service`.

Each service's `internal/api/routes.go` wires these per endpoint via an exported `Routes(...) http.Handler` (signature varies per service — e.g. `api-service`'s takes the JWT secret and all three of its services) — the same function `cmd/main.go` and tests both call, so tests exercise the real middleware chain instead of a re-implementation of it. Endpoint types are wired differently: endpoints meant to be hit directly by a client (`files`, `login`) treat a request ID as mandatory (`RequestMiddleware(log, true)`, paired with `RequireRequestID()` or a generating `RequestID` middleware), while `/health` does not (`RequestMiddleware(log, false)`), since external health probes (e.g. a kubelet liveness check) won't send one.
