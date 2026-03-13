#!/bin/sh

set -eu

repo_root=$(git rev-parse --show-toplevel)

if [ ! -f "$repo_root/.gitmodules" ]; then
	exit 0
fi

tmpdir=$(mktemp -d)
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

git clone --quiet --no-checkout "file://$repo_root" "$tmpdir/repo"
git -C "$tmpdir/repo" checkout --quiet HEAD
git -C "$tmpdir/repo" submodule sync --recursive >/dev/null
git -C "$tmpdir/repo" submodule update --init --recursive --depth 1
