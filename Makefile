GO ?= mise exec -- go
GO_TEST ?= $(GO) test ./internal/providers ./internal/pullpreview ./internal/providers/hetzner
DIST_DIR := dist
BIN_NAME := pullpreview
VERSION_FILE := internal/pullpreview/constants.go
GO_LDFLAGS ?= -s -w
UPX ?= upx
UPX_FLAGS ?= --best --lzma
DIST_COMMIT_MESSAGE ?= chore(dist): update bundled pullpreview binary
RELEASE_REMOTE ?= origin
RELEASE_BRANCH ?= v6
VERSION_NUMBER := $(patsubst v%,%,$(VERSION))
VERSION_TAG := v$(VERSION_NUMBER)

DIST_BINARIES := \
	$(DIST_DIR)/$(BIN_NAME)-linux-amd64

.NOTPARALLEL: bump dist tag release

.PHONY: bump bump-check clean-dist dist dist-check dist-commit release rewrite tag tag-check test version-check

dist: dist-check clean-dist $(DIST_BINARIES) dist-commit

version-check:
	@if [ -z "$(VERSION)" ]; then \
		echo "VERSION is required. Example: make bump VERSION=v6.2.0"; \
		exit 1; \
	fi

bump-check:
	@if [ -n "$$(git status --porcelain --untracked-files=no)" ]; then \
		echo "Refusing to bump version with uncommitted tracked changes."; \
		git status --short --untracked-files=no; \
		exit 1; \
	fi

bump: version-check bump-check
	@set -eu; \
	version="$(VERSION_NUMBER)"; \
	perl -0pi -e 's/Version\s+=\s+"[^"]+"/Version   = "'$$version'"/' $(VERSION_FILE); \
	if git diff --quiet -- $(VERSION_FILE); then \
		echo "$(VERSION_FILE) already set to $$version."; \
		exit 0; \
	fi; \
	$(GO_TEST); \
	git add $(VERSION_FILE); \
	git commit -m "Bump version to $$version" -- $(VERSION_FILE)

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

tag-check: version-check
	@set -eu; \
	if [ -n "$$(git status --porcelain --untracked-files=no)" ]; then \
		echo "Refusing to tag with uncommitted tracked changes."; \
		git status --short --untracked-files=no; \
		exit 1; \
	fi; \
	current_version="$$(sed -n 's/.*Version   = \"\\([^\"]*\\)\"/\\1/p' $(VERSION_FILE))"; \
	if [ "$$current_version" != "$(VERSION_NUMBER)" ]; then \
		echo "Version mismatch: $(VERSION_FILE) has $$current_version, expected $(VERSION_NUMBER)."; \
		exit 1; \
	fi; \
	if git rev-parse -q --verify "refs/tags/$(VERSION_TAG)" >/dev/null; then \
		echo "Tag $(VERSION_TAG) already exists."; \
		exit 1; \
	fi

tag: tag-check
	git tag -a $(VERSION_TAG) -m "$(VERSION_TAG)"

release: bump dist tag
	@set -eu; \
	current_branch="$$(git rev-parse --abbrev-ref HEAD)"; \
	if [ "$$current_branch" = "HEAD" ]; then \
		echo "Refusing to release from detached HEAD."; \
		exit 1; \
	fi; \
	git push $(RELEASE_REMOTE) "$$current_branch" "$(VERSION_TAG)"; \
	if [ "$$current_branch" = "$(RELEASE_BRANCH)" ]; then \
		exit 0; \
	fi; \
	git fetch $(RELEASE_REMOTE) "$(RELEASE_BRANCH)"; \
	git switch $(RELEASE_BRANCH) >/dev/null 2>&1 || git switch -c $(RELEASE_BRANCH) --track $(RELEASE_REMOTE)/$(RELEASE_BRANCH) >/dev/null; \
	trap 'git switch "$$current_branch" >/dev/null' EXIT; \
	git merge --ff-only "$(RELEASE_REMOTE)/$(RELEASE_BRANCH)"; \
	git merge --ff-only "$$current_branch"; \
	git push $(RELEASE_REMOTE) $(RELEASE_BRANCH); \
	trap - EXIT; \
	git switch "$$current_branch" >/dev/null

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
	$(GO_TEST)
