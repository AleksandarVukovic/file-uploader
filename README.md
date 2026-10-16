# file-uploader

A Go mono repo of independent microservices for uploading files to S3. `api-service` is the only publicly exposed service; it forwards uploads to `file-service`, which stores them in S3 and publishes a `FileUploaded` event to RabbitMQ, and checks logins against `user-service`, which owns user accounts in PostgreSQL. `file-processing-service` is still at skeleton stage.

## Requirements

- Go 1.26+
- Docker (required for `make test-e2e` and `file-service`'s mTLS integration tests; optional otherwise, e.g. for building images or `make run`)
- AWS credentials with access to the target S3 bucket (for `file-service`)

## Services

| Service                   | Exposed | Purpose                                        |
| -------------------------- | ------- | ----------------------------------------------- |
| `api-service`              | Public  | Gateway; validates and forwards file uploads; issues JWTs on login after checking credentials with `user-service` (see [Authentication](#authentication)) |
| `file-service`             | Internal| Streams uploaded files to S3 and publishes `FileUploaded` events to RabbitMQ (see [File events](#file-events)) |
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

`api-service` validates and caps the body at 10Mb before forwarding it to `file-service`, which validates the checksum, assigns the UUID and records the file's metadata (owner, original filename, content type, size, checksum, S3 location, status) in its own PostgreSQL database (`file-db`) *before* uploading. The row starts as `pending`; `file-service` then uploads to S3 via `PutObject` under the key `<userID>/<uuid>` (the UUID returned in the response), passing the checksum through for S3-side integrity verification, and marks the row `uploaded` once the put succeeds, recording a `FileUploaded` event in the `outbox_events` table in the same database transaction (so a file is never `uploaded` without its event, or the reverse). If the put fails, the `pending` row is deleted again (using a context detached from the request, so the cleanup survives a cancelled request).

A user can't upload the same content twice: `(user_id, checksum_sha256)` is unique, so a repeat upload is rejected as `409 conflict` when the `pending` row is inserted, before anything reaches S3.

A crash between the insert and the put (or a failed status update after a successful put) leaves a `pending` row behind. `storage.Service`'s `CleanupPendingFiles` sweeps rows that have been `pending` longer than the TTL (`--pendingFileTTL` / `PENDING_FILE_TTL`, default 1800 seconds), in batches of 100: if the object exists in S3 the row is marked `uploaded` (in the same transaction as the upload path, so the `FileUploaded` event is recorded too), otherwise it is deleted. Nothing schedules this sweep yet.

The `FileUploaded` events recorded in `outbox_events` are published to RabbitMQ by a background poller, see [File events](#file-events).

`file-service` is layered handler → `storage` service → object store / repository: the handler handles HTTP, the `storage` service (`file-service/internal/service/storage`) validates the checksum, assigns the UUID, builds the key and manages the row's lifecycle through the `storage.Repository` interface (`file-service/internal/repository/files`, sqlc over pgx), and the object store is only reached through the `storage.ObjectStore` interface (`Put`, `Exists`), which the `storage` service declares and `file-service/internal/objectstore` implements (`objectstore.NewS3` is the S3 implementation), so another backend can be added without touching the handler or the service.

On success the endpoint responds `201 Created` with a JSON body describing the stored file:

```json
{"uuid": "0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11", "filename": "users.csv", "contentType": "text/csv", "size": 1048576}
```

## File events

`file-service` publishes the `FileUploaded` events it records in `outbox_events` (the transactional outbox) to RabbitMQ, so a consumer such as `file-processing-service` can react to uploads. A background poller in `file-service/cmd/main.go` runs every 2 seconds and calls the `broker` service (`file-service/internal/service/broker`), which:

1. fetches up to `--brokerBatchSize` (`BROKER_BATCH_SIZE`, default 100) unpublished events, oldest first, only of aggregate type `file` (other event types will get their own publishers), with `FOR UPDATE SKIP LOCKED`;
2. publishes each one and waits for the broker's publisher confirm;
3. sets `published_at` on the row only after the broker has confirmed it.

Delivery is at-least-once: a crash between the confirm and `published_at` being set, or several `file-service` replicas polling at once, can publish an event twice, so consumers must deduplicate on the message ID. A failure on one event is returned (and logged by the poller) while the rest of the batch carries on, and the event is retried on the next poll. An event with an unknown type is never published and is reported on every poll.

Each message goes to the durable topic exchange `ex.files` with routing key `file.uploaded`, as persistent `application/json`. Its message ID is the outbox event's ID, its `type` is `FileUploaded`, and the body is `common/events.FileUploadedPayload`:

```json
{"fileId": "0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11", "userId": 7, "rootDir": "bucket", "path": "7/0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11", "contentType": "text/csv", "size": 1048576, "checksum": "<hex SHA-256>"}
```

Messages are not published as `mandatory`, so an exchange without a matching binding would silently drop them; the bindings come from `infra/rabbitmq/definitions.json`, which also declares the vhost `file-uploader`, the quorum queue `q.file.uploaded` (bound with `file.uploaded`) and a dead-letter queue `q.file.uploaded.dead` fed through `ex.files.dlx` after three failed deliveries.

The code is split so each layer only depends inwards: `file-service/internal/outbox` holds the shared `Event` type and `Repository` interface (implemented by `internal/repository/outbox`, used by both the `storage` and `broker` services), and `common/messaging` holds the `Message` type, the `Publisher` interface and `NewRabbitMQPublisher`, so other services can publish the same way.

`file-service` needs the broker at startup and exits if it can't connect. Configuration (env var or flag):

| Env var                 | Flag                  | Default    | Purpose                                                        |
| ----------------------- | --------------------- | ---------- | -------------------------------------------------------------- |
| `BROKER_URL`            | `brokerURL`           |            | RabbitMQ URL including the vhost, e.g. `amqps://rabbitmq:5671/file-uploader` |
| `BROKER_FILES_EXCHANGE` | `brokerFilesExchange` | `ex.files` | Exchange the events are published to                           |
| `BROKER_TLS_CERT_FILE`  | `brokerTLSCertFile`   |            | Client certificate presented to RabbitMQ; its CN is the RabbitMQ user |
| `BROKER_TLS_KEY_FILE`   | `brokerTLSKeyFile`    |            | Private key of that client certificate                         |
| `BROKER_BATCH_SIZE`     | `brokerBatchSize`     | `100`      | Maximum number of events published per poll                    |

The broker's server certificate is verified against `TLS_CA_CERT_FILE`. See `file-service/.env.example`.

## Authentication

`api-service` exposes `POST /login`, which accepts a JSON body `{"username": "...", "password": "..."}` and returns `{"token": "<jwt>"}` on success. Login checks the credentials by calling `user-service`'s `Authenticate` over gRPC with mutual TLS, forwarding the request's `x-request-id`. A wrong password and an unknown user both come back as `401 invalid_credentials`, and an unreachable `user-service` as `503 unavailable`. `api-service` still signs the token itself rather than `user-service`; moving token issuance there is a possible follow-up. `POST /register` creates an account: it takes `{"username": "...", "email": "...", "password": "..."}` and calls `user-service`'s `Create`, returning `201` with `{"id": ..., "username": "...", "email": "..."}`. It is public and unversioned like `/login`, and runs the same middleware chain. The body is validated against the same rules as `user-service` (lowercase username, 8–72 printable ASCII password), and `user-service` additionally rejects reserved usernames and passwords missing an uppercase letter, a lowercase letter, a digit or a special character (`400 invalid_input`, with the reason in the message). A taken username or email gives `409 user_exists` (one response for both, so it doesn't say which field clashed), and an unreachable `user-service` gives `503`. Registration is not rate-limited yet, and every attempt costs a bcrypt hash, so put a limit in front of it before exposing it publicly.

The returned JWT is HS256-signed using the `JWT_SECRET` environment variable, with a 15-minute expiry (`sub` set to the user's numeric ID, plus standard `iat`/`exp` claims). `api-service` **requires `JWT_SECRET` to be set** and will refuse to start without it.

`POST /api/v1/files/upload` requires a valid token via `Authorization: Bearer <token>`, enforced by `api-service/internal/middleware.JWT`, which rejects tokens whose `sub` isn't a numeric user ID. `api-service` forwards that ID to `file-service` in an `X-User-Id` header. `POST /login` and `GET /health` are intentionally exempt: `/login` is how a client obtains a token in the first place, and `/health` is probed by infra (e.g. a kubelet) that never presents one.

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

RabbitMQ is TLS-only too (`infra/rabbitmq/rabbitmq.conf`): there is no plaintext listener, only port 5671 with TLS 1.2/1.3, and a client must present a certificate signed by the shared CA. Clients authenticate with that certificate alone (SASL `EXTERNAL`), the certificate's CN is the RabbitMQ user, and the users have no password. Permissions are least-privilege: `file-service` may only write to `ex.files`, and `file-processing-service` may only read `q.file.uploaded`.

## Running locally

```sh
make certs
make run
```

`make certs` generates a local development CA and leaf certificates for `api-service`, `file-service`, `user-service`, the `user-db` and `file-db` Postgres instances and RabbitMQ (a `rabbitmq` server certificate plus one client certificate each for `file-service` and `file-processing-service`, named `rabbitmq-<service>.pem`) under `certs/` (gitignored). Run it once before the first `make run`, and again if `certs/` is ever deleted — the services will refuse to start without a valid cert/key/CA on disk.

`make run` builds and starts `api-service`, `file-service`, `user-service`, their PostgreSQL databases (`user-db`, `file-db`) and RabbitMQ via `docker-compose.local.yml`; one-shot `user-db-migrate` and `file-db-migrate` containers apply the goose migrations before `user-service` and `file-service` start respectively, and `file-service` also waits for RabbitMQ to be healthy. `file-service` mounts your local `~/.aws` credentials read-only, so make sure `AWS_PROFILE` is configured on the host. Copy each service's `.env.example` to `.env` and fill in the required values (`S3_BUCKET_NAME`, `AWS_PROFILE`, `AWS_REGION`, `JWT_SECRET`) — the root `.env` feeds `docker-compose.local.yml` directly, and `api-service` will refuse to start without `JWT_SECRET` set. The local database user, password and name default to `user_service`/`user_service`/`users` and can be overridden with `USER_DB_USER`, `USER_DB_PASSWORD` and `USER_DB_NAME`; `file-db` works the same way with `file_service`/`file_service`/`files` and `FILE_DB_USER`, `FILE_DB_PASSWORD` and `FILE_DB_NAME`.

There are no users in a fresh database, so create one with `POST /register` (for example `curl -X POST localhost:8080/register -H 'Content-Type: application/json' -d '{"username":"john_doe","email":"john.doe@example.com","password":"Correct-Horse-42"}'`) before trying `/login`. The default container ports are 8080 (HTTP) and 8443 (mTLS); `user-service`'s health port is published on host port `8082`, its gRPC port isn't published, and RabbitMQ's TLS port is published on `5671`.

To run migrations by hand from `user-service/`, export `GOOSE_DRIVER`, `GOOSE_DBSTRING` and `GOOSE_MIGRATION_DIR` (see `user-service/.env.example`) and use `make migrate-up`, `migrate-down`, `migrate-status` or `migrate-create NAME=...`.

## End-to-end tests

`test/integration/` holds a Ginkgo+Gomega end-to-end suite (build-tagged `e2e`) that brings up a real `api-service` + `user-service` + `file-service` stack, plus PostgreSQL for `user-service` and `file-service`, RabbitMQ and a LocalStack container (S3 only), using testcontainers-go's compose module, then drives it over plain HTTP exactly like a real client would — register, login, then upload — asserting against the object actually stored in S3 and the event that reaches RabbitMQ rather than mocks. It only ever calls `api-service`'s public port; the S3 and RabbitMQ checks are out-of-band assertions.

```sh
make test-e2e
```

This regenerates certs (`make certs`) and runs `go test -tags=e2e ./test/integration/...`; it needs Docker and isn't part of `make`/`make all`, since it builds and starts real containers rather than running in-process. `file-service` picks up an optional `S3_ENDPOINT` env var (unused — and a no-op — in production) to point its S3 client at LocalStack instead of real AWS for this suite.

The stack also includes `user-service` and its PostgreSQL (TLS-only, with the migrations applied by a one-shot goose container), so the suite exercises the real `/register` → `/login` → upload path. `BeforeSuite` registers the test user through `POST /register` rather than seeding the database directly.

`BeforeSuite` starts the stack in two phases: LocalStack first, to create the test bucket, then everything else. `file-service` checks that its S3 bucket is reachable on startup and exits if it isn't, so the bucket has to exist before it starts. Since one user can't upload the same content twice, specs that upload use `uniqueTestdata` (the fixture plus a unique row) so their checksums differ.

The `File events` spec uploads a file, then connects to RabbitMQ from the test host with the `file-processing-service` client certificate and reads `q.file.uploaded` until the event for that upload shows up, asserting the message properties and payload. Events from the other specs' uploads land in the same queue, and the spec reads past them.

## Make targets

Run from the repo root:

| Target                          | Description                                                       |
| -------------------------------- | ------------------------------------------------------------------ |
| `make`                           | Build+test every service and `common/`                             |
| `make certs`                     | Generate local dev CA + leaf certs for mTLS between services and for RabbitMQ |
| `make run`                       | Run `api-service` + `file-service` + `user-service`, their databases and RabbitMQ locally via docker compose |
| `make test-e2e`                  | Run the `test/integration/` e2e suite against a real api-service + user-service + file-service + PostgreSQL + RabbitMQ + LocalStack stack (see [End-to-end tests](#end-to-end-tests)) |
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

`user-service` and `file-service` (the services with a database) include the shared `db.mk`, which adds `migrate-status`, `migrate-up`, `migrate-down` and `migrate-create NAME=<name>` (goose, configured through `GOOSE_*` env vars) and `sqlc-generate` (regenerates `internal/repository/gen` from `db/`); `user-service`'s own `Makefile` also adds `protoc-gen-go`/`protoc-gen-go-grpc` (install the protobuf plugins `make generate` needs; make sure `$(go env GOPATH)/bin` is on your `PATH`).

The image name and tag can be overridden:

```sh
make docker-push DOCKER_REPO=<user>/<project> DOCKER_IMAGE_TAG=v1.0.0
```

Note: **Docker build context is always the repo root** — always invoke `docker build`/`make docker-build` from the root, not the service directory.

## Database transactions

Writes that must commit together (today: `file-service` marking a file `uploaded` and inserting its `FileUploaded` outbox event) use the transactor in `common/db`:

- `db.NewTransactor(pool).WithinTx(ctx, fn)` begins a transaction, passes `fn` a context carrying it, and commits if `fn` returns nil; an error or panic rolls back. A nested `WithinTx` joins the outer transaction.
- `db.NewExecutor(pool)` is a sqlc-compatible `DBTX` that runs each query on the transaction found in the context, or on the pool when there is none, so repositories stay stateless and join a transaction simply by being called with the context handed to `fn`.

## Logging

`common/logger` provides a JSON `slog` logger that you store in and read from a `context.Context`:

```go
log := logger.NewLogger(debug)
ctx := logger.WithCtx(ctx, log)

logger.FromCtx(ctx).Info("server started", "port", port)
```

`FromCtx` panics if the context has no logger, so attach one early (e.g. in `main` or in middleware). `middleware.Logger(log, mandatory bool)` (`common/http/middleware`) always attaches a per-request logger to the context — tagged with the request ID once one has been populated on the context, without that tag otherwise. If `mandatory` is `true` and no request ID is present by the time this middleware runs, it panics instead of silently logging without one, so a route wired with `mandatory=true` must have a request-ID-populating middleware (goa's `RequestID` or `RequireContextHeaders`, see below) ahead of it in the chain.

## Request IDs

`common/http/middleware` propagates an `X-Request-Id` header across service calls:

- `RequireContextHeaders()` rejects internal-service requests that arrive without an `X-Request-Id` or a numeric `X-User-Id` header, and stores both in the context (`middleware.RequestIDKey`, `middleware.UserIDFromCtx`). Used by `file-service`'s files endpoint, which is never called directly by clients. The calling side is `common/http/client.NewDoer`, which copies the request ID and user ID from the context into those headers.
- `PanicHandler()` recovers from panics in HTTP handlers and returns a 500 instead of crashing the process.
- `common/http/client.NewDoer` returns a goa HTTP `Doer` that forwards the request ID from context onto outgoing requests, so `api-service` calling into `file-service` preserves the same ID end-to-end.

The gRPC hop to `user-service` carries the ID as `x-request-id` metadata (gRPC metadata keys are lowercase), using `common/grpc/interceptor`:

- `ClientRequestID()` — a client interceptor on `api-service`'s connection that copies the ID from the context into outgoing metadata.
- `RequestID(required)` — a server interceptor that reads the metadata value into the context, rejecting calls without it as `InvalidArgument` when `required` is true (the counterpart of `RequireRequestID()`).
- `Logger(log, mandatory)` — a server interceptor that tags the request-scoped logger with `reqID` from the context (the counterpart of `middleware.Logger`; it panics if `mandatory` is true and no ID is present, so `RequestID` must come first in the chain).
- `Recover(log)` — turns a panic in a gRPC handler into an `Internal` status and logs it.

`/login` therefore runs goa's `RequestID` middleware (and `Logger(log, true)`), since it has to forward an ID to `user-service`.

Each service's `internal/api/routes.go` wires these per endpoint via an exported `Routes(...) http.Handler` (signature varies per service — e.g. `api-service`'s takes the JWT secret and all three of its handlers) — the same function `cmd/main.go` and tests both call, so tests exercise the real middleware chain instead of a re-implementation of it. Endpoint types are wired differently: endpoints meant to be hit directly by a client (`files`, `login`) treat a request ID as mandatory (`Logger(log, true)`, paired with `RequireContextHeaders()` or a generating `RequestID` middleware), while `/health` does not (`Logger(log, false)`), since external health probes (e.g. a kubelet liveness check) won't send one.
