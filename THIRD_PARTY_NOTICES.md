# Third-Party Notices

The CLI statically links the following components. Release packages include
their complete license texts in `THIRD_PARTY_LICENSES/`.

## FFmpeg

- Version: 8.1.2
- Source: https://ffmpeg.org/
- License: LGPL-2.1-or-later for the distributed build. The build scripts do
  not enable `--enable-gpl`, `--enable-version3`, or `--enable-nonfree`.
- Full license text: `THIRD_PARTY_LICENSES/FFmpeg-LGPL-2.1-or-later.txt`
- Build configuration: `scripts/bootstrap-static-audio-deps.sh`

## Opus

- Version: 1.5.2
- Source: https://opus-codec.org/
- License: BSD-3-Clause
- Full license text: `THIRD_PARTY_LICENSES/Opus-BSD-3-Clause.txt`
- Build configuration: `scripts/bootstrap-static-audio-deps.sh`

## Project License

- GPL-3.0-or-later
