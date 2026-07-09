# libav shim contract

The top-level Go `libav` backend calls this package's Go wrapper. This package owns all cgo usage and links against C functions named `sa_*_with_threads_ctx`.

These functions must be implemented with FFmpeg libraries directly, not by spawning `ffmpeg`, `ffprobe`, or `mediainfo` processes.

Required implementation mapping:

- `sa_probe_duration_with_threads_ctx`: `libavformat` stream/format probing.
- `sa_volume_detect_with_threads_ctx`: decode audio, normalize to mono double samples, and compute mean/max dB locally.
- `sa_silence_detect_with_threads_ctx`: decode audio, normalize to mono double samples, and compute silence intervals locally with the supplied threshold and duration.
- `sa_transcode_wav_with_threads_ctx`: decode, resample to mono target sample rate, encode PCM S16LE WAV.
- `sa_split_wav_fixed_with_threads_ctx`: use FFmpeg's segment muxing behavior in-process to produce fixed-duration WAV slices, equivalent to the Python `-f segment -segment_time ... -segment_format wav -reset_timestamps 1` flow.
- `sa_export_wav_with_threads_ctx`: seek/export a continuous interval as mono WAV.
- `sa_render_intervals_wav_with_threads_ctx`: build an `asplit + atrim + asetpts + concat` filtergraph from ordered intervals and render mono WAV.
- `sa_concat_wav_with_threads_ctx`: concatenate same-format WAV inputs in-process. Prefer concat demuxer semantics when formats are copy-compatible; fall back to a concat filtergraph when direct concat fails, matching the Python service behavior.
- `sa_encode_audio_with_threads_ctx`: encode mono audio using the requested container, codec, bitrate, and sample format.

The C layer owns FFmpeg object lifetimes and returns heap-allocated interval arrays to the wrapper. The wrapper releases those arrays through `sa_free`.

The shim must avoid global mutable FFmpeg log capture for concurrent calls. Fixed-slice trimming depends on backend calls being safe under concurrent Go workers.
