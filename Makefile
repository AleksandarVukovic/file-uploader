PROJECT_ROOT=$(shell git rev-parse --show-toplevel)
PROJECT_NAME=$(shell basename $(PROJECT_ROOT))

DOCKER_REPO?=vukovic96/$(PROJECT_NAME)
DOCKER_IMAGE_TAG?=latest

all: test coverage build

build:
	go build -o ./bin/$(PROJECT_NAME) ./cmd/; \

test: goenv fmt vet gotest

gotest:
	@files=$$(go list ./... | grep -v /gen | grep -v /design | grep -v /cmd | grep -v /test); \
	go test -v -race -timeout=30s $$files

fmt:
	go fmt ./...

vet:
	go vet ./...

goenv:
	@go version

coverage:
	@files=$$(go list ./... | grep -v /gen | grep -v /design | grep -v /cmd | grep -v /test); \
	go test -coverprofile=coverage.out $$files; \
	go tool cover -html=coverage.out -o coverage.html
	@total=$$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}'); \
	echo "Total coverage: $$total"; \
	if [ $$(echo "$$total < 70.0" | sed 's/%//g' | bc) -eq 1 ]; then \
		echo "ERROR: Coverage is below 70%!"; \
		exit 1; \
	fi

clean:
	rm -rf $(PROJECT_ROOT)/bin
	rm -rf coverage.out coverage.html

docker-build:
	docker build . -t $(DOCKER_REPO):$(DOCKER_IMAGE_TAG)

docker-push: docker-build
	docker push $(DOCKER_REPO):$(DOCKER_IMAGE_TAG)