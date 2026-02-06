GO ?= mise exec -- go
DIST_DIR := dist
BIN_NAME := pullpreview
GO_LDFLAGS ?= -s -w
UPX ?= upx
UPX_FLAGS ?= --best --lzma
DIST_COMMIT_MESSAGE ?= chore(dist): update bundled pullpreview binary

DIST_BINARIES := \
	$(DIST_DIR)/$(BIN_NAME)-linux-amd64

.PHONY: dist dist-check dist-commit clean-dist test

dist: dist-check clean-dist $(DIST_BINARIES) dist-commit

dist-check:
	@if [ -n "$$(git status --porcelain --untracked-files=no -- . ':(exclude)$(DIST_DIR)')" ]; then \
		echo "Refusing to build dist with uncommitted source changes."; \
		echo "Commit changes first, then run 'make dist'."; \
		git status --short -- . ':(exclude)$(DIST_DIR)'; \
		exit 1; \
	fi

$(DIST_DIR)/$(BIN_NAME)-linux-amd64:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(GO_LDFLAGS)' -o $@ ./cmd/pullpreview
	$(UPX) $(UPX_FLAGS) $@

dist-commit:
	git add -A $(DIST_DIR)
	@if git diff --cached --quiet -- $(DIST_DIR); then \
		echo "No dist changes to commit."; \
	else \
		git commit -m "$(DIST_COMMIT_MESSAGE)" -- $(DIST_DIR); \
	fi

clean-dist:
	rm -f $(DIST_DIR)/$(BIN_NAME)-linux-amd64 $(DIST_DIR)/$(BIN_NAME)-linux-arm64

test:
	$(GO) test ./...
