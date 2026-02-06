GO ?= mise exec -- go
DIST_DIR := dist
BIN_NAME := pullpreview

DIST_BINARIES := \
	$(DIST_DIR)/$(BIN_NAME)-linux-amd64 \
	$(DIST_DIR)/$(BIN_NAME)-linux-arm64

.PHONY: dist clean-dist test

dist: clean-dist $(DIST_BINARIES)

$(DIST_DIR)/$(BIN_NAME)-linux-amd64:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -o $@ ./cmd/pullpreview

$(DIST_DIR)/$(BIN_NAME)-linux-arm64:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -o $@ ./cmd/pullpreview

clean-dist:
	rm -f $(DIST_DIR)/$(BIN_NAME)-linux-amd64 $(DIST_DIR)/$(BIN_NAME)-linux-arm64

test:
	$(GO) test ./...
