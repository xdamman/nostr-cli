BINARY_NAME := nostr
MODULE := github.com/xdamman/nostr-cli

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
COMMIT_DATE ?= $(shell git log -1 --format=%ci 2>/dev/null || echo "unknown")
COMMIT_MSG ?= $(shell git log -1 --format=%s 2>/dev/null || echo "unknown")

LDFLAGS := -X '$(MODULE)/cmd.Version=$(VERSION)' \
           -X '$(MODULE)/cmd.CommitSHA=$(COMMIT_SHA)' \
           -X '$(MODULE)/cmd.CommitDate=$(COMMIT_DATE)' \
           -X '$(MODULE)/cmd.CommitMsg=$(COMMIT_MSG)'

.PHONY: build install clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

install:
	go build -ldflags "$(LDFLAGS)" -o $(GOPATH)/bin/$(BINARY_NAME) .

clean:
	rm -f $(BINARY_NAME)
