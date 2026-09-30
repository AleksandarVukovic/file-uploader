# For a single service: make -C api-service <target>

SERVICES = api-service \
			file-service \
			file-processing-service \
			user-service

.PHONY: all all-services run common-pkg-test

# Build each service by calling `make all`
all: common-pkg-test
	@set -e; \
	for service in $(SERVICES); do \
		echo "********************************"; \
		echo "*** Building $$service ***"; \
		echo "********************************"; \
		$(MAKE) -C $$service all; \
	done

# Run a given command in each service: make all-services <target>
all-services:
	@set -e; \
	if [ -z "$(TARGET)" ]; then \
		echo "Usage: make all-services TARGET=<target>"; \
		exit 1; \
	fi; \
	for service in $(SERVICES); do \
		echo "********************************"; \
		echo "*** Running make $(TARGET) in $$service ***"; \
		echo "********************************"; \
		$(MAKE) -C $$service $(TARGET); \
	done

run:
	docker compose -f docker-compose.local.yml up --build

common-pkg-test:
	@echo "********************************"; \
	echo "*** Testing common packages ***"; \
	echo "********************************"
	go vet ./common/...
	go test -v -race -timeout=30s ./common/...
