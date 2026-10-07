# Shared targets for every microservice.

PROJECT_ROOT := $(shell git rev-parse --show-toplevel)
PROJECT_NAME := $(shell basename $(PROJECT_ROOT))
GO_MODULE_NAME := github.com/aleksandarv/file-uploader

ifndef SERVICE_NAME
@$(error SERVICE_NAME is not set — define it in the service's Makefile before including common.mk)
endif

SERVICE_DIR := $(PROJECT_ROOT)/$(SERVICE_NAME)

DOCKER_REPO ?= vukovic96/$(PROJECT_NAME)-$(SERVICE_NAME)
DOCKER_IMAGE_TAG ?= latest

BIN_DIR ?= $(SERVICE_DIR)/bin
COVERAGE_THRESHOLD ?= 70.0

TEST_EXCLUDE ?= /(gen|design|cmd|test)(/|$$)
TEST_PKGS = $(shell go list ./... | grep -vE '$(TEST_EXCLUDE)')
INTEGRATION_TEST_TIMEOUT ?= 60s

# goa variables
GOA_VERSION := v3.32.0
GOA_CMD := goa.design/goa/v3/cmd/goa
GOA_DESIGN_PKG ?= $(GO_MODULE_NAME)/$(SERVICE_NAME)/design
GOA_GEN_OUTPUT ?= $(SERVICE_DIR)

.PHONY: all build test gotest test-integration gotest-integration fmt vet goenv coverage clean docker-build docker-push goa-install generate

all: test coverage gotest-integration build

build:
	go build -o $(BIN_DIR)/$(SERVICE_NAME) ./cmd/

test: goenv fmt vet gotest

gotest:
	go test -v -race -timeout=30s $(TEST_PKGS)

test-integration: goenv fmt vet gotest-integration

# run both i.e. unit and integration tests
gotest-integration:
	go test -tags=integration -v -race -timeout=$(INTEGRATION_TEST_TIMEOUT) $(TEST_PKGS)

fmt:
	go fmt ./...

vet:
	go vet ./...

goenv:
	@go version

coverage:
	go test -coverprofile=coverage.out $(TEST_PKGS)
	go tool cover -html=coverage.out -o coverage.html
	@total=$$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}' | tr -d %) && \
	echo "Total coverage: $$total%" && \
	if [ $$(echo "$$total < $(COVERAGE_THRESHOLD)" | bc) -eq 1 ]; then \
		echo "ERROR: Coverage is below $(COVERAGE_THRESHOLD)%!"; \
		exit 1; \
	fi

clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html

docker-build:
	docker build -f $(CURDIR)/Dockerfile -t $(DOCKER_REPO):$(DOCKER_IMAGE_TAG) $(PROJECT_ROOT)

docker-push: docker-build
	docker push $(DOCKER_REPO):$(DOCKER_IMAGE_TAG)

goa-install:
	go install $(GOA_CMD)@$(GOA_VERSION)
	go get $(GOA_CMD)@$(GOA_VERSION)
	go mod tidy

generate: goa-install
	go run $(GOA_CMD) gen $(GOA_DESIGN_PKG) -o $(GOA_GEN_OUTPUT)