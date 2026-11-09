VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -s -w -X github.com/asbjorn/spondcli/internal/cli.version=$(VERSION)

.PHONY: build install test vet fmt check clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/spondcli ./cmd/spondcli

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/spondcli

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

clean:
	rm -rf bin
