#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FFMPEG_DIR="${FFMPEG_DIR:-$ROOT_DIR/third_party/src/ffmpeg}"
PREFIX="${PREFIX:-$ROOT_DIR/third_party/ffmpeg-audio}"

cd "$FFMPEG_DIR"

./configure \
  --prefix="$PREFIX" \
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
  --enable-filter=anull

make -j"${JOBS:-$(getconf _NPROCESSORS_ONLN)}"
make install
