GO ?= go
VERSION := $(shell tr -d '[:space:]' < VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build monitor test vet check clean

all: check build

build:
	mkdir -p bin
	GOTOOLCHAIN=local CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/sbackup ./cmd/sbackup

monitor:
	mkdir -p bin
	GOTOOLCHAIN=local CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/sbackup-monitor ./cmd/sbackup-monitor

test:
	GOTOOLCHAIN=local $(GO) test -buildvcs=false ./...

vet:
	GOTOOLCHAIN=local $(GO) vet -buildvcs=false ./...

check: test vet
	bash -n scripts/*.sh

clean:
	rm -f bin/sbackup bin/sbackup-monitor
