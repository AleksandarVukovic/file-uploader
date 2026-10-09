SERVICE_NAME := file-service

GOOSE_VERSION=v3.27.2
SQLC_VERSION=v1.31.1

.PHONY: goose-install migrate-status migrate-up migrate-down migrate-create sqlc-install sqlc-generate

test-integration: certs

certs:
	$(MAKE) -C $(PROJECT_ROOT) certs

goose-install:
	go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

# ensure that GOOSE_DRIVER, GOOSE_DBSTRING and GOOSE_MIGRATION_DIR are set
migrate-status: goose-install
	goose status

migrate-up: goose-install
	goose up

migrate-down: goose-install
	goose down

migrate-create: goose-install
	goose -s create $(NAME) sql

sqlc-install:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

sqlc-generate: sqlc-install
	cd $(SERVICE_DIR)/db && sqlc generate

include ../common.mk