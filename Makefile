.PHONY: build test test-e2e clean docker docker-build run-node

BINARY ?= azghost
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/azghost

test:
	go test ./...

# Unit-only packages that e2e depends on (no live stack required).
test-client:
	go test ./client ./e2e/e2eharness -count=1

# Published example live tests (//go:build e2e). Requires AzerothCore + MySQL.
# Env overrides: E2E_AUTH_ADDR, E2E_AUTH_DSN, E2E_CHAR_DSN, E2E_WORLD_DSN
test-e2e-examples:
	go test -tags=e2e ./e2e/examples -count=1 -v -timeout 30m -parallel 2

# Private local suite under e2e/local/ (gitignored). Full AC regression set.
test-e2e-local:
	go test -tags=e2e ./e2e/local -count=1 -v -timeout 30m -parallel 2

# Alias: examples only (what CI/downstream may copy patterns from).
test-e2e: test-e2e-examples

test-race:
	go test ./... -race

clean:
	rm -f $(BINARY) $(BINARY)-* *.test coverage.out

docker docker-build:
	docker build --build-arg VERSION=$(VERSION) -t azghost:$(VERSION) -t azghost:latest .

run-node: build
	./$(BINARY) node --listen :8888 --data-dir "$${AC_DATA_DIR:-./ac-data}"
