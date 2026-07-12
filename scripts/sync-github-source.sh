#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_DIR="${XDP_GITHUB_DIR:-$ROOT_DIR/xdp-github-release}"

if [ "$TARGET_DIR" = "$ROOT_DIR" ] || [ "$TARGET_DIR" = "/" ]; then
  printf 'refusing to sync into unsafe target: %s\n' "$TARGET_DIR" >&2
  exit 1
fi

rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"

rsync -a --delete \
  --include='/README.md' \
  --include='/build/' \
  --include='/build/plugin-packages/' \
  --include='/build/plugin-packages/kafka-input-sample.zip' \
  --include='/build/plugin-packages/json-parser-sample.zip' \
  --include='/build/plugin-packages/json-parser-sample-1.1.0.zip' \
  --include='/build/plugin-packages/table-search-command-sample.zip' \
  --include='/build/plugin-packages/sort-search-command-sample.zip' \
  --include='/build/plugin-packages/head-search-command-sample.zip' \
  --include='/build/plugin-packages/dedup-search-command-sample.zip' \
  --exclude='/build/**' \
  --exclude='*.md' \
  --exclude='/.git/' \
  --exclude='/.cache/' \
  --exclude='/.agents/' \
  --exclude='/.codex/' \
  --exclude='/.code-review-graph/' \
  --exclude='/.claude/' \
  --exclude='/.gitee/' \
  --exclude='/data/' \
  --exclude='/docs/' \
  --exclude='/github/' \
  --exclude='/xdp-github-release/' \
  --exclude='/xdp-source-*/' \
  --exclude='/xdp-source-*.tar.gz' \
  --exclude='/web/console/node_modules/' \
  --exclude='/web/console/dist/' \
  --exclude='/.DS_Store' \
  --exclude='*/.DS_Store' \
  "$ROOT_DIR/" "$TARGET_DIR/"

markdown_files="$(find "$TARGET_DIR" -name '*.md' -print)"
if [ "$markdown_files" != "$TARGET_DIR/README.md" ]; then
  printf 'unexpected markdown files in %s:\n%s\n' "$TARGET_DIR" "$markdown_files" >&2
  exit 1
fi

artifact_matches="$(
  find "$TARGET_DIR" \
    -path '*/.git' -o \
    -path '*/.cache' -o \
    -path '*/.code-review-graph' -o \
    -path '*/.claude' -o \
    -path '*/.gitee' -o \
    -path '*/build/docker-bin' -o \
    -path '*/data' -o \
    -path '*/node_modules' -o \
    -path '*/dist' -o \
    -path '*/.DS_Store' \
    -print
)"
if [ -n "$artifact_matches" ]; then
  printf 'unexpected generated/local artifacts in %s:\n%s\n' "$TARGET_DIR" "$artifact_matches" >&2
  exit 1
fi

printf 'GitHub source directory synced: %s\n' "$TARGET_DIR"
du -sh "$TARGET_DIR"
