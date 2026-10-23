#!/usr/bin/env sh
set -eu
ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
PREFIX="${PREFIX:-$ROOT_DIR/third_party/ffmpeg-audio}"
CC="${CC:-cc}"
PKG_CONFIG_BIN="${PKG_CONFIG_BIN:-pkg-config}"
# Prefer the local build even when MSYS2 or the caller already sets a search path.
export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/smartaudio-pssh.XXXXXXXX")"
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
# Intentional splitting of pkg-config compiler/linker flags.
cflags="$("$PKG_CONFIG_BIN" --cflags libavformat libavcodec libavutil)"
libs="$("$PKG_CONFIG_BIN" --static --libs libavformat libavcodec libavutil)"
"$CC" -std=c11 -Wall -Wextra -Werror $cflags \
  "$ROOT_DIR/scripts/tests/mov-pssh.c" -o "$test_dir/mov-pssh.exe" $libs
"$test_dir/mov-pssh.exe" "$test_dir/pssh.mov"
