# libav shim contract

The top-level Go `libav` backend calls this package's Go wrapper. This package owns all cgo usage and links against C functions named `sa_*`.

These functions must be implemented with FFmpeg libraries directly, not by spawning `ffmpeg`, `ffprobe`, or `mediainfo` processes.

Required implementation mapping:

- `sa_probe_duration`: `libavformat` stream/format probing.
- `sa_volume_detect`: decode audio, normalize to mono double samples, and compute mean/max dB locally.
- `sa_silence_detect`: decode audio, normalize to mono double samples, and compute silence intervals locally with the supplied threshold and duration.
- `sa_transcode_wav`: decode, resample to mono target sample rate, encode PCM S16LE WAV.
- `sa_split_wav_fixed`: use FFmpeg's segment muxing behavior in-process to produce fixed-duration WAV slices, equivalent to the Python `-f segment -segment_time ... -segment_format wav -reset_timestamps 1` flow.
- `sa_export_wav`: seek/export a continuous interval as mono WAV.
- `sa_render_intervals_wav`: build an `asplit + atrim + asetpts + concat` filtergraph from ordered intervals and render mono WAV.
- `sa_concat_wav`: concatenate same-format WAV inputs in-process. Prefer concat demuxer semantics when formats are copy-compatible; fall back to a concat filtergraph when direct concat fails, matching the Python service behavior.
- `sa_encode_opus`: encode mono OGG/Opus using `libopus`.

The C layer owns FFmpeg object lifetimes and returns heap-allocated interval arrays to the wrapper. The wrapper releases those arrays through `sa_free`.

The shim must avoid global mutable FFmpeg log capture for concurrent calls. Fixed-slice trimming depends on backend calls being safe under concurrent Go workers.
