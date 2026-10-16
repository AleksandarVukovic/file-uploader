# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26.8 AS builder

WORKDIR /app

# copy only what we need from file-processing-service to our app folder
COPY ./file-processing-service/cmd ./app/file-processing-service/cmd
COPY ./file-processing-service/internal ./app/file-processing-service/internal
COPY ./file-processing-service/gen ./app/file-processing-service/gen
COPY ./file-processing-service/Makefile ./app/file-processing-service/Makefile
# copy only what we need from root folder
COPY ./common ./app/common
COPY ./common.mk ./app/common.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app/file-processing-service build PROJECT_NAME=file-processing-service

# final stage
FROM alpine:3.24.2

RUN addgroup -S appgroup && adduser -S -G appgroup appuser

WORKDIR /home/appuser

COPY --from=builder --chown=appuser:appgroup /file-processing-service/bin/file-processing-service ./file-processing-service

ENV HEALTH_PORT=8080

EXPOSE $HEALTH_PORT

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
CMD wget -q -O /dev/null "http://localhost:$HEALTH_PORT/health" || exit 1

USER appuser

CMD ["./file-processing-service"]
