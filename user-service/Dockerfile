# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26.8 AS builder

WORKDIR /app

# copy only what we need from user-service to our app folder
COPY ./user-service/cmd ./app/cmd
COPY ./user-service/internal ./app/internal
COPY ./user-service/Makefile ./app/
# copy only what we need from root folder
COPY ./common ./common
COPY ./common.mk ./common.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app build PROJECT_NAME=user-service

# final stage
FROM alpine:3.24.2

RUN addgroup -S appgroup && adduser -S -G appgroup appuser

WORKDIR /home/appuser

COPY --from=builder --chown=appuser:appgroup /user-service/bin/user-service ./user-service

ENV PORT=8080

EXPOSE $PORT

USER appuser

CMD ["./user-service"]
