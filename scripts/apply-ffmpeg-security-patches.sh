#!/usr/bin/env sh
# Apply the audited backport before either bootstrap or standalone builds.
set -eu
ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FFMPEG_DIR="${1:?Usage: apply-ffmpeg-security-patches.sh FFMPEG_SOURCE_DIR}"
PATCH_FILE="$ROOT_DIR/scripts/patches/ffmpeg-8.1.3-cve-2026-107678.patch"
cd "$FFMPEG_DIR"
if [ "$(cat RELEASE)" != "8.1.3" ]; then
  printf 'Security patches require FFmpeg 8.1.3.\n' >&2
  exit 1
fi
# Check the source itself: a stamp could incorrectly accept a partially patched
# tree. Dry runs prevent partial application, and reverse checks allow rebuilds.
# Force the reverse direction: --batch may silently switch individual files
# back to forward application and incorrectly accept a partially patched tree.
if patch --batch --forward --dry-run --fuzz=0 -p1 < "$PATCH_FILE"; then
  patch --batch --forward --fuzz=0 -p1 < "$PATCH_FILE"
elif patch --force --reverse --dry-run --fuzz=0 -p1 < "$PATCH_FILE"; then
  printf 'FFmpeg CVE-2026-107678 backport is already applied.\n'
else
  printf 'FFmpeg security patch does not match the source; refusing to build.\n' >&2
  exit 1
fi
