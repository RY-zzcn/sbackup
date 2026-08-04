GO ?= go
VERSION := $(shell tr -d '[:space:]' < VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build monitor test race vet shellcheck check clean

all: check build monitor

build:
	mkdir -p bin
	GOTOOLCHAIN=local CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/sbackup ./cmd/sbackup

monitor:
	mkdir -p bin
	GOTOOLCHAIN=local CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/sbackup-monitor ./cmd/sbackup-monitor

test:
	GOTOOLCHAIN=local $(GO) test -buildvcs=false ./...

race:
	GOTOOLCHAIN=local CGO_ENABLED=1 $(GO) test -race -buildvcs=false ./...

vet:
	GOTOOLCHAIN=local $(GO) vet -buildvcs=false ./...

shellcheck:
	shellcheck scripts/*.sh

check: test vet shellcheck
	bash -n scripts/*.sh

clean:
	rm -f bin/sbackup bin/sbackup-monitor
