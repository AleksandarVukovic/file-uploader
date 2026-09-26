# ==============================================================================
# NOTE: running `make docker-build` sets the build context to the ROOT directory!
# ==============================================================================

# build stage
FROM golang:1.26 AS builder

WORKDIR /app

# copy only what we need from file-processing-service to our app folder
COPY ./file-processing-service/cmd ./app/cmd
COPY ./file-processing-service/internal ./app/internal
COPY ./file-processing-service/Makefile ./app/
# copy only what we need from root folder
COPY ./common ./common
COPY ./common.mk ./common.mk
COPY ./go.mod ./go.sum ./app/

# build app by running make command from root folder
RUN CGO_ENABLED=0 GOOS=linux make -C app build PROJECT_NAME=file-processing-service

# final stage
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /file-processing-service/bin/file-processing-service ./file-processing-service

ENV PORT=8080

EXPOSE $PORT

CMD ["./file-processing-service"]
