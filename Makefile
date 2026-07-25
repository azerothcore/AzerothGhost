.PHONY: build test clean docker docker-build run-node

BINARY ?= azghost
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/azghost

test:
	go test ./...

test-race:
	go test ./... -race

clean:
	rm -f $(BINARY) $(BINARY)-* *.test coverage.out

docker docker-build:
	docker build --build-arg VERSION=$(VERSION) -t azghost:$(VERSION) -t azghost:latest .

run-node: build
	./$(BINARY) node --listen :8888 --data-dir "$${AC_DATA_DIR:-./ac-data}"
