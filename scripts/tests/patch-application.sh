#!/usr/bin/env sh
# Exercise fresh, repeated and partially applied backports using real sources.
set -eu
ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
SOURCE_DIR="${1:?Usage: patch-application.sh FFMPEG_SOURCE_DIR}"
SOURCE_DIR="$(CDPATH= cd -- "$SOURCE_DIR" && pwd)"
PATCH_FILE="$ROOT_DIR/scripts/patches/ffmpeg-8.1.3-cve-2026-107678.patch"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/smartaudio-patch.XXXXXXXX")"
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
files='RELEASE libavutil/encryption_info.c libavutil/encryption_info.h libavutil/tests/encryption_info.c libavformat/isom.h libavformat/mov.c'
for file in $files; do
  mkdir -p "$test_dir/$(dirname "$file")"
  cp "$SOURCE_DIR/$file" "$test_dir/$file"
done
"$ROOT_DIR/scripts/apply-ffmpeg-security-patches.sh" "$test_dir"
cd "$test_dir"
patch --force --reverse --fuzz=0 -p1 < "$PATCH_FILE"
cp libavformat/mov.c mov-original.c
"$ROOT_DIR/scripts/apply-ffmpeg-security-patches.sh" "$test_dir"
sha256sum $files > patched.sha256
"$ROOT_DIR/scripts/apply-ffmpeg-security-patches.sh" "$test_dir"
sha256sum --check patched.sha256

# A partially reverted tree must fail without modifying any source file.
cp mov-original.c libavformat/mov.c
sha256sum $files > partial.sha256
if "$ROOT_DIR/scripts/apply-ffmpeg-security-patches.sh" "$test_dir"; then
  printf 'ERROR: accepted a partially patched FFmpeg tree.\n' >&2
  exit 1
fi
sha256sum --check partial.sha256
printf 'FFmpeg patch application regressions passed.\n'
