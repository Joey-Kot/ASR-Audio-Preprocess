#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
THIRD_PARTY_DIR="${THIRD_PARTY_DIR:-$ROOT_DIR/third_party}"
SRC_DIR="${SRC_DIR:-$THIRD_PARTY_DIR/src}"
PREFIX="${PREFIX:-$THIRD_PARTY_DIR/ffmpeg-audio}"

FFMPEG_VERSION="${FFMPEG_VERSION:-8.1.2}"
OPUS_VERSION="${OPUS_VERSION:-1.5.2}"

FFMPEG_ARCHIVE_EXT="${FFMPEG_ARCHIVE_EXT:-tar.gz}"
FFMPEG_URL="${FFMPEG_URL:-https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.$FFMPEG_ARCHIVE_EXT}"
FFMPEG_SIG_URL="${FFMPEG_SIG_URL:-$FFMPEG_URL.asc}"
FFMPEG_PGP_KEY_URL="${FFMPEG_PGP_KEY_URL:-https://ffmpeg.org/ffmpeg-devel.asc}"
FFMPEG_PGP_FINGERPRINT="${FFMPEG_PGP_FINGERPRINT:-FCF986EA15E6E293A5644F10B4322F04D67658D8}"
OPUS_URL="${OPUS_URL:-https://downloads.xiph.org/releases/opus/opus-$OPUS_VERSION.tar.gz}"
FFMPEG_SHA256="${FFMPEG_SHA256:-}"
OPUS_SHA256="${OPUS_SHA256:-}"

JOBS="${JOBS:-$(getconf _NPROCESSORS_ONLN)}"
CC="${CC:-cc}"
PKG_CONFIG_BIN="${PKG_CONFIG_BIN:-pkg-config}"

mkdir -p "$SRC_DIR" "$PREFIX"

download() {
  url="$1"
  out="$2"
  if [ -f "$out" ]; then
    return 0
  fi
  curl --fail --show-error --location -o "$out" "$url"
}

verify_sha256() {
  file="$1"
  expected="$2"
  if [ -z "$expected" ]; then
    return 0
  fi
  actual="$(sha256sum "$file" | awk '{print $1}')"
  if [ "$actual" != "$expected" ]; then
    printf '%s\n' "sha256 mismatch for $file"
    printf '%s\n' "expected: $expected"
    printf '%s\n' "actual:   $actual"
    exit 1
  fi
}

verify_gpg_signature() {
  file="$1"
  signature="$2"
  key_file="$3"
  expected_fingerprint="$4"

  if ! gpg_bin="$(command -v gpg)"; then
    printf '%s\n' "gpg is required to verify $file"
    exit 1
  fi

  gnupg_home="$SRC_DIR/gnupg"
  mkdir -p "$gnupg_home"
  chmod 700 "$gnupg_home"

  fingerprints="$(GNUPGHOME="$gnupg_home" "$gpg_bin" --batch --show-keys --with-colons "$key_file" | awk -F: '$1 == "fpr" { print $10 }')"
  found_fingerprint=0
  for fingerprint in $fingerprints; do
    if [ "$fingerprint" = "$expected_fingerprint" ]; then
      found_fingerprint=1
      break
    fi
  done

  if [ "$found_fingerprint" != 1 ]; then
    printf '%s\n' "FFmpeg PGP key fingerprint mismatch"
    printf '%s\n' "expected: $expected_fingerprint"
    printf '%s\n' "found:    $fingerprints"
    exit 1
  fi

  GNUPGHOME="$gnupg_home" "$gpg_bin" --batch --import "$key_file"
  verify_output="$(GNUPGHOME="$gnupg_home" "$gpg_bin" --batch --status-fd 1 --verify "$signature" "$file")" || {
    printf '%s\n' "$verify_output"
    exit 1
  }
  signed_by_expected=0
  for signed_fingerprint in $(printf '%s\n' "$verify_output" | awk -v expected="$expected_fingerprint" '$1 == "[GNUPG:]" && $2 == "VALIDSIG" && ($3 == expected || $NF == expected) { print expected }'); do
    if [ "$signed_fingerprint" = "$expected_fingerprint" ]; then
      signed_by_expected=1
      break
    fi
  done
  if [ "$signed_by_expected" != 1 ]; then
    printf '%s\n' "FFmpeg signature was not made by the expected PGP key"
    printf '%s\n' "expected: $expected_fingerprint"
    printf '%s\n' "$verify_output"
    exit 1
  fi
}

extract_archive() {
  archive="$1"
  dest="$2"

  case "$archive" in
    *.tar.gz | *.tgz)
      tar --no-same-owner -xzf "$archive" -C "$dest"
      ;;
    *.tar.xz | *.txz)
      tar --no-same-owner -xJf "$archive" -C "$dest"
      ;;
    *.tar.bz2 | *.tbz2)
      tar --no-same-owner -xjf "$archive" -C "$dest"
      ;;
    *)
      printf '%s\n' "unsupported archive format: $archive"
      exit 1
      ;;
  esac
}

build_opus() {
  archive="$SRC_DIR/opus-$OPUS_VERSION.tar.gz"
  source_dir="$SRC_DIR/opus-$OPUS_VERSION"
  download "$OPUS_URL" "$archive"
  verify_sha256 "$archive" "$OPUS_SHA256"
  if [ ! -f "$source_dir/.extract-ok" ]; then
    rm -rf "$source_dir"
    extract_archive "$archive" "$SRC_DIR"
    touch "$source_dir/.extract-ok"
  fi
  cd "$source_dir"
  ./configure \
    --prefix="$PREFIX" \
    --disable-shared \
    --enable-static \
    --disable-extra-programs \
    --disable-doc \
    ${OPUS_CONFIGURE_FLAGS:-}
  make -j"$JOBS"
  make install
}

check_opus_pkg_config() {
  PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig" "$PKG_CONFIG_BIN" --static --modversion opus
  PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig" "$PKG_CONFIG_BIN" --static --cflags --libs opus
}

build_ffmpeg() {
  archive="$SRC_DIR/ffmpeg-$FFMPEG_VERSION.$FFMPEG_ARCHIVE_EXT"
  signature="$archive.asc"
  key_file="$SRC_DIR/ffmpeg-devel.asc"
  source_dir="$SRC_DIR/ffmpeg-$FFMPEG_VERSION"
  download "$FFMPEG_URL" "$archive"
  verify_sha256 "$archive" "$FFMPEG_SHA256"
  if [ -n "$FFMPEG_SIG_URL" ]; then
    download "$FFMPEG_SIG_URL" "$signature"
    download "$FFMPEG_PGP_KEY_URL" "$key_file"
    verify_gpg_signature "$archive" "$signature" "$key_file" "$FFMPEG_PGP_FINGERPRINT"
  fi
  if [ ! -f "$source_dir/.extract-ok" ]; then
    rm -rf "$source_dir"
    extract_archive "$archive" "$SRC_DIR"
    touch "$source_dir/.extract-ok"
  fi
  cd "$source_dir"
  if ! PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig" ./configure \
    --prefix="$PREFIX" \
    --cc="$CC" \
    --pkg-config="$PKG_CONFIG_BIN" \
    --pkg-config-flags="--static" \
    --extra-cflags="-I$PREFIX/include" \
    --extra-ldflags="-L$PREFIX/lib" \
    --enable-static \
    --disable-shared \
    --disable-programs \
    --disable-doc \
    --disable-debug \
    --disable-network \
    --disable-autodetect \
    --disable-everything \
    --disable-iamf \
    --disable-x86asm \
    --enable-small \
    ${FFMPEG_CONFIGURE_FLAGS:-} \
    --enable-avcodec \
    --enable-avformat \
    --enable-avfilter \
    --enable-swresample \
    --enable-protocol=file \
    --enable-demuxer=wav \
    --enable-demuxer=mp3 \
    --enable-demuxer=aac \
    --enable-demuxer=mov \
    --enable-demuxer=matroska \
    --enable-demuxer=ogg \
    --enable-demuxer=flac \
    --enable-demuxer=concat \
    --enable-muxer=wav \
    --enable-muxer=ogg \
    --enable-muxer=flac \
    --enable-muxer=adts \
    --enable-muxer=mp4 \
    --enable-muxer=segment \
    --enable-parser=aac \
    --enable-parser=mpegaudio \
    --enable-parser=opus \
    --enable-parser=vorbis \
    --enable-parser=flac \
    --enable-decoder=pcm_s16le \
    --enable-decoder=pcm_s24le \
    --enable-decoder=pcm_s32le \
    --enable-decoder=pcm_f32le \
    --enable-decoder=mp3 \
    --enable-decoder=aac \
    --enable-decoder=flac \
    --enable-decoder=opus \
    --enable-decoder=vorbis \
    --enable-decoder=alac \
    --enable-encoder=pcm_s16le \
    --enable-encoder=pcm_s24le \
    --enable-encoder=pcm_s32le \
    --enable-encoder=pcm_f32le \
    --enable-encoder=flac \
    --enable-encoder=aac \
    --enable-encoder=libopus \
    --enable-libopus \
    --enable-filter=aformat \
    --enable-filter=aresample \
    --enable-filter=asplit \
    --enable-filter=atrim \
    --enable-filter=asetpts \
    --enable-filter=concat \
    --enable-filter=silencedetect \
    --enable-filter=volumedetect \
    --enable-filter=anull; then
    if [ -f ffbuild/config.log ]; then
      tail -n 200 ffbuild/config.log
    fi
    exit 1
  fi
  make -j"$JOBS"
  make install
}

build_opus
check_opus_pkg_config
build_ffmpeg

printf '%s\n' "Static audio dependencies installed at: $PREFIX"
printf '%s\n' "export PKG_CONFIG_PATH=\"$PREFIX/lib/pkgconfig\""
printf '%s\n' "export PKG_CONFIG=\"pkg-config --static\""
