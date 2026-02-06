GO ?= mise exec -- go
DIST_DIR := dist
BIN_NAME := pullpreview
GO_LDFLAGS ?= -s -w
UPX ?= upx
UPX_FLAGS ?= --best --lzma
DIST_COMMIT_MESSAGE ?= chore(dist): update bundled pullpreview binary

DIST_BINARIES := \
	$(DIST_DIR)/$(BIN_NAME)-linux-amd64

.PHONY: dist dist-check dist-commit clean-dist rewrite test

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
	rm -f $(DIST_DIR)/$(BIN_NAME)-linux-amd64

rewrite:
	@set -eu; \
	current_branch="$$(git rev-parse --abbrev-ref HEAD)"; \
	if [ "$$current_branch" = "HEAD" ]; then \
		echo "Refusing to rewrite detached HEAD."; \
		exit 1; \
	fi; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Working tree must be clean before rewrite."; \
		git status --short; \
		exit 1; \
	fi; \
	base_ref="$${BASE_REF:-$$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD || echo origin/master)}"; \
	merge_base="$$(git merge-base "$$base_ref" "$$current_branch")"; \
	safe_branch="$$(printf '%s' "$$current_branch" | tr '/' '-')"; \
	tmp_branch="rewrite-$$safe_branch-$$(date +%s)"; \
	commits_file="$$(mktemp)"; \
	trap 'rm -f "$$commits_file"' EXIT; \
	git rev-list --reverse "$$merge_base..$$current_branch" > "$$commits_file"; \
	echo "Rewriting $$current_branch onto $$base_ref (merge-base $$merge_base)"; \
	git checkout -b "$$tmp_branch" "$$merge_base" >/dev/null; \
	while IFS= read -r sha; do \
		[ -z "$$sha" ] && continue; \
		subject="$$(git show -s --format=%s "$$sha")"; \
		files="$$(git show --pretty=format: --name-only "$$sha" | sed '/^$$/d')"; \
		if [ "$$subject" = "$(DIST_COMMIT_MESSAGE)" ] && [ -n "$$files" ] && ! printf '%s\n' "$$files" | grep -qv '^dist/'; then \
			echo "Dropping dist commit $$sha ($$subject)"; \
			continue; \
		fi; \
		git cherry-pick "$$sha" >/dev/null; \
	done < "$$commits_file"; \
	git branch -f "$$current_branch" HEAD; \
	git checkout "$$current_branch" >/dev/null; \
	git branch -D "$$tmp_branch" >/dev/null; \
	echo "Rewrite complete. Force-push with: git push --force-with-lease origin $$current_branch"

test:
	$(GO) test ./...
