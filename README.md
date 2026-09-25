# golang-starter

A minimal template for starting new Go services. It provides a basic project layout, a structured logger, a Makefile for common tasks, and a Dockerfile, so a new project can start with application code right away.

## Using this template

1. Create a new repository from this template and clone it.
2. Update the module path in `go.mod` to your own (e.g. `github.com/<user>/<project>`).
3. Replace `golang-starter` in the `Dockerfile` with your project name.
4. Set `DOCKER_REPO` in the `Makefile`, or pass it on the command line.
5. Replace this README with a description of your project.

## Requirements

- Go 1.26+
- Docker (optional, for building images)

## Make targets

| Target             | Description                                                  |
| ------------------ | ------------------------------------------------------------ |
| `make`             | Run tests, check coverage and build                          |
| `make build`       | Build the binary into `./bin/`                               |
| `make test`        | Run `go fmt`, `go vet` and tests with the race detector      |
| `make coverage`    | Generate a coverage report; fails if total coverage is below 70% |
| `make clean`       | Remove build and coverage artifacts                          |
| `make docker-build`| Build the Docker image                                       |
| `make docker-push` | Build and push the Docker image                              |

The image name and tag can be overridden:

```sh
make docker-push DOCKER_REPO=<user>/<project> DOCKER_IMAGE_TAG=v1.0.0
```

## Logging

`internal/logger` provides a JSON `slog` logger that you store in and read from a `context.Context`:

```go
log := logger.NewLogger(debug)
ctx := logger.WithCtx(ctx, log)

logger.FromCtx(ctx).Info("server started", "port", port)
```

`FromCtx` panics if the context has no logger, so attach one early (e.g. in `main` or in middleware).
