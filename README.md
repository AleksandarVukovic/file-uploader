# file-uploader

A Go mono repo of independent microservices for uploading files to S3. `api-service` is the only publicly exposed service; it forwards uploads to `file-service`, which stores them in S3. `user-service` and `file-processing-service` are still at skeleton stage.

## Requirements

- Go 1.26+
- Docker (optional, for building images / running locally)
- AWS credentials with access to the target S3 bucket (for `file-service`)

## Services

| Service                   | Exposed | Purpose                                        |
| -------------------------- | ------- | ----------------------------------------------- |
| `api-service`              | Public  | Gateway; validates and forwards file uploads    |
| `file-service`             | Internal| Streams uploaded files to S3                    |
| `user-service`             | Internal| User operations, auth tokens (skeleton)         |
| `file-processing-service`  | Internal| File processing (skeleton)                      |

Each service exposes:

- `GET /health` — health check, returns `{"status": "ok"}`
- `POST /api/v1/files/upload` — uploads a single file (`api-service`, `file-service`)

### File upload

Uploads are streamed rather than buffered, with metadata carried in headers:

| Header                 | Payload field | Notes                                  |
| ----------------------- | -------------- | --------------------------------------- |
| `Content-Disposition`   | `filename`     | Must end in `.csv`                      |
| `Content-Length`        | `size`         | Max 10Mb                                 |
| `Content-Type`          | `contentType`  | Must be `text/csv`                       |
| `X-Checksum-Sha256`     | `checksum`     | Hex-encoded SHA-256 digest of the body   |

`api-service` validates and caps the body at 10Mb before forwarding it to `file-service`, which verifies the checksum and uploads to S3 via `PutObject`, passing the checksum through for S3-side integrity verification.

## Running locally

```sh
make run
```

This builds and starts `api-service` and `file-service` via `docker-compose.local.yml`. `file-service` mounts your local `~/.aws` credentials read-only, so make sure `AWS_PROFILE` is configured on the host. Copy each service's `.env.example` to `.env` and fill in the required values (`S3_BUCKET_NAME`, `AWS_PROFILE`, `AWS_REGION`).

## Make targets

Run from the repo root:

| Target                          | Description                                                       |
| -------------------------------- | ------------------------------------------------------------------ |
| `make`                           | Build+test every service and `common/`                             |
| `make run`                       | Run `api-service` + `file-service` locally via docker compose       |
| `make all-services TARGET=<t>`   | Run `make <t>` in every service                                    |
| `make common-pkg-test`           | Vet + race-test only `common/...`                                  |

Run from a single service directory (or `make -C <service> <target>` from the root):

| Target             | Description                                                  |
| ------------------ | ------------------------------------------------------------ |
| `make`             | Run tests, check coverage and build                          |
| `make build`       | Build the binary into `./bin/`                               |
| `make test`        | Run `go fmt`, `go vet` and tests with the race detector      |
| `make coverage`    | Generate a coverage report; fails if total coverage is below 70% |
| `make clean`       | Remove build and coverage artifacts                          |
| `make generate`    | Run `goa gen` against the service's `design` package         |
| `make docker-build`| Build the Docker image                                       |
| `make docker-push` | Build and push the Docker image                              |

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

`FromCtx` panics if the context has no logger, so attach one early (e.g. in `main` or in middleware). `logger.RequestMiddleware` attaches a per-request logger tagged with the request ID once one has been populated on the context (see `common/http/middleware`).

## Request IDs

`common/http/middleware` propagates an `X-Request-Id` header across service calls:

- `RequireRequestID()` rejects internal-service requests that arrive without one (used by `file-service`, which is never called directly by clients).
- `PanicHandler()` recovers from panics in HTTP handlers and returns a 500 instead of crashing the process.
- `common/http/client.NewDoer` returns a goa HTTP `Doer` that forwards the request ID from context onto outgoing requests, so `api-service` calling into `file-service` preserves the same ID end-to-end.
