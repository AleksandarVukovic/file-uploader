# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26.8 AS builder

WORKDIR /app

# copy only what we need from user-service to our app folder
COPY ./user-service/cmd ./app/user-service/cmd
COPY ./user-service/internal ./app/user-service/internal
COPY ./user-service/gen ./app/user-service/gen
COPY ./user-service/Makefile ./app/user-service/Makefile
# copy only what we need from root folder
COPY ./common ./app/common
COPY ./common.mk ./app/common.mk
COPY ./db.mk ./app/db.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app/user-service build PROJECT_NAME=user-service

# final stage
FROM alpine:3.24.2

RUN addgroup -S appgroup && adduser -S -G appgroup appuser

WORKDIR /home/appuser

COPY --from=builder --chown=appuser:appgroup /user-service/bin/user-service ./user-service

ENV HEALTH_PORT=8080

EXPOSE $HEALTH_PORT

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
CMD wget -q -O /dev/null "http://localhost:$HEALTH_PORT/health" || exit 1

USER appuser

CMD ["./user-service"]
