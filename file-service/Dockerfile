# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26.8 AS builder

WORKDIR /app

# copy only what we need from file-service to our app folder
COPY ./file-service/cmd ./app/cmd
COPY ./file-service/internal ./app/internal
COPY ./file-service/Makefile ./app/
# copy only what we need from root folder
COPY ./common ./common
COPY ./common.mk ./common.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app build PROJECT_NAME=file-service

# final stage
FROM alpine:3.24.2

RUN addgroup -S appgroup && adduser -S -G appgroup appuser

WORKDIR /home/appuser

COPY --from=builder --chown=appuser:appgroup /file-service/bin/file-service ./file-service

ENV PORT=8080

EXPOSE $PORT

USER appuser

CMD ["./file-service"]
