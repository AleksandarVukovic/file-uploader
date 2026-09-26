# For a single service: make -C api-service <target>

SERVICES = api-service \
			file-service \
			file-processing-service \
			user-service

.PHONY: all common-pkg-test

# Build each service by calling `make all`
all: common-pkg-test
	@set -e; \
	for service in $(SERVICES); do \
		echo "********************************"; \
		echo "*** Building $$service ***"; \
		echo "********************************"; \
		$(MAKE) -C $$service all; \
	done

common-pkg-test:
	@echo "********************************"; \
	echo "*** Testing common packages ***"; \
	echo "********************************"
	go vet ./common/...
	go test -v -race -timeout=30s ./common/...
