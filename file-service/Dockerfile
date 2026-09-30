# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26.8 AS builder

WORKDIR /app

# copy only what we need from file-service to our app folder
COPY ./file-service/cmd ./app/file-service/cmd
COPY ./file-service/internal ./app/file-service/internal
COPY ./file-service/gen ./app/file-service/gen
COPY ./file-service/Makefile ./app/file-service/Makefile
# copy only what we need from root folder
COPY ./common ./app/common
COPY ./common.mk ./app/common.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app/file-service build PROJECT_NAME=file-service

# final stage
FROM alpine:3.24.2

RUN addgroup -S appgroup && adduser -S -G appgroup appuser

WORKDIR /home/appuser

COPY --from=builder --chown=appuser:appgroup /file-service/bin/file-service ./file-service

ENV PORT=8080

EXPOSE $PORT

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
CMD wget -q -O /dev/null "http://localhost:$PORT/health" || exit 1

USER appuser

CMD ["./file-service"]
