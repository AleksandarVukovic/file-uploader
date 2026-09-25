# build stage
FROM golang:1.26 AS builder

WORKDIR /app

# copy only what we need
COPY ./cmd ./cmd
COPY ./internal ./internal
COPY go.mod go.sum Makefile ./

RUN CGO_ENABLED=0 GOOS=linux make build PROJECT_NAME=file-uploader

# final stage
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/bin/file-uploader ./file-uploader

ENV PORT=8080

EXPOSE $PORT

CMD ["./file-uploader"]