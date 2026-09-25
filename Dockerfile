# NOTE: Replace every occurrence of `golang-starter` in this file with your project name.
# PROJECT_NAME is passed to make explicitly because .git isn't copied into the image, so the Makefile
# can't derive it from the repository directory name.

# build stage
FROM golang:1.26 AS builder

WORKDIR /app

# copy only what we need
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY go.mod go.sum Makefile ./

RUN CGO_ENABLED=0 GOOS=linux make build PROJECT_NAME=golang-starter

# final stage
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/bin/golang-starter ./golang-starter

ENV PORT=8080

EXPOSE $PORT

CMD ["./golang-starter"]