#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel)"
CERTS_DIR="$ROOT_DIR/certs"
DAYS=825

mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

echo "Generating local dev CA..."
openssl genrsa -out ca-key.pem 2048
openssl req -x509 -new -nodes -key ca-key.pem -sha256 -days "$DAYS" \
  -subj "/CN=file-uploader-dev-ca" -out ca.pem

echo "Generating file-service server certificate..."
openssl genrsa -out file-service-key.pem 2048
openssl req -new -key file-service-key.pem -subj "/CN=file-service" -out file-service.csr
openssl x509 -req -in file-service.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
  -out file-service.pem -days "$DAYS" -sha256 \
  -extfile <(printf "subjectAltName=DNS:file-service,DNS:localhost\nextendedKeyUsage=serverAuth")

echo "Generating user-service server certificate..."
openssl genrsa -out user-service-key.pem 2048
openssl req -new -key user-service-key.pem -subj "/CN=user-service" -out user-service.csr
openssl x509 -req -in user-service.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
  -out user-service.pem -days "$DAYS" -sha256 \
  -extfile <(printf "subjectAltName=DNS:user-service,DNS:localhost\nextendedKeyUsage=serverAuth")

echo "Generating api-service client certificate..."
openssl genrsa -out api-service-key.pem 2048
openssl req -new -key api-service-key.pem -subj "/CN=api-service" -out api-service.csr
openssl x509 -req -in api-service.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
  -out api-service.pem -days "$DAYS" -sha256 \
  -extfile <(printf "extendedKeyUsage=clientAuth")

echo "Generating user-db server certificate..."
openssl genrsa -out user-db-key.pem 2048
openssl req -new -key user-db-key.pem -subj "/CN=user-db" -out user-db.csr
openssl x509 -req -in user-db.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
  -out user-db.pem -days "$DAYS" -sha256 \
  -extfile <(printf "subjectAltName=DNS:user-db,DNS:localhost\nextendedKeyUsage=serverAuth")

echo "Generating file-db server certificate..."
openssl genrsa -out file-db-key.pem 2048
openssl req -new -key file-db-key.pem -subj "/CN=file-db" -out file-db.csr
openssl x509 -req -in file-db.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
  -out file-db.pem -days "$DAYS" -sha256 \
  -extfile <(printf "subjectAltName=DNS:file-db,DNS:localhost\nextendedKeyUsage=serverAuth")

rm -f file-service.csr user-service.csr api-service.csr user-db.csr file-db.csr ca.srl
chmod 644 ca.pem user-db.pem user-db-key.pem file-db.pem file-db-key.pem file-service.pem file-service-key.pem user-service.pem user-service-key.pem api-service.pem api-service-key.pem

echo "Done. Certs written to $CERTS_DIR"
