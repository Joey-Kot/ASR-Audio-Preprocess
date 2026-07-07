#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
THIRD_PARTY_DIR="${THIRD_PARTY_DIR:-$ROOT_DIR/third_party}"
SRC_DIR="${SRC_DIR:-$THIRD_PARTY_DIR/src}"
PREFIX="${PREFIX:-$THIRD_PARTY_DIR/ffmpeg-audio}"

FFMPEG_VERSION="${FFMPEG_VERSION:-7.1.1}"
OPUS_VERSION="${OPUS_VERSION:-1.5.2}"

FFMPEG_URL="${FFMPEG_URL:-https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz}"
OPUS_URL="${OPUS_URL:-https://downloads.xiph.org/releases/opus/opus-$OPUS_VERSION.tar.gz}"

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
  curl -L -o "$out" "$url"
}

build_opus() {
  archive="$SRC_DIR/opus-$OPUS_VERSION.tar.gz"
  source_dir="$SRC_DIR/opus-$OPUS_VERSION"
  download "$OPUS_URL" "$archive"
  if [ ! -f "$source_dir/.extract-ok" ]; then
    rm -rf "$source_dir"
    tar --no-same-owner -xzf "$archive" -C "$SRC_DIR"
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
  archive="$SRC_DIR/ffmpeg-$FFMPEG_VERSION.tar.xz"
  source_dir="$SRC_DIR/ffmpeg-$FFMPEG_VERSION"
  download "$FFMPEG_URL" "$archive"
  if [ ! -f "$source_dir/.extract-ok" ]; then
    rm -rf "$source_dir"
    tar --no-same-owner -xJf "$archive" -C "$SRC_DIR"
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
    --enable-encoder=flac \
    --enable-encoder=aac \
    --enable-encoder=libopus \
    --enable-libopus \
    --enable-filter=abuffer \
    --enable-filter=abuffersink \
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
