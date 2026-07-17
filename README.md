# ASR Audio Preprocess

[English](README.md) | [简体中文](README_ZH.md)

ASR Audio Preprocess is a Go audio-preprocessing library with a thin CLI entry point. It makes silence trimming, concurrent fixed-slice processing, ordered merging, and silence-based ASR request segmentation reusable.

Go projects can import the library directly. Other projects can use the same processing capability through the prebuilt CLI binary.

## Features

- Usable as a Go library or CLI
- Uses statically linked FFmpeg/libav in-process; it never invokes external `ffmpeg`, `ffprobe`, or `mediainfo` commands
- libav performs WAV conversion, duration and volume probing, silence detection, trimming, slicing, merging, and segment encoding
- Concurrently trims fixed-length slices and merges valid results in their original order
- Produces segments suitable for concurrent ASR requests
- Returns structured processing information for logging and monitoring

## Processing pipeline

```text
Input audio
  -> Convert to WAV at a uniform sample rate
  -> Split into fixed-length slices
  -> Concurrently trim long silence from every slice
  -> Merge valid slices in original order
  -> Split into ASR segments by silence intervals and maximum request length
  -> Output the requested segment format and processing information
```

Go handles the API, configuration, concurrency, ordered assembly, and return values. FFmpeg/libav handles decoding, filtering, trimming, muxing, and encoding.

## Default configuration

`DefaultConfig()` returns:

| Setting | Default | Description |
| --- | --- | --- |
| `Silence.MinSilence` | `700ms` (`DefaultSilentInterval`) | Minimum silence interval |
| `Silence.Padding` | `100ms` (`DefaultPadding`) | Padding retained on both sides of speech |
| `Silence.ThresholdDB` | `nil` | Fixed silence threshold; when `nil`, candidates are generated from volume statistics |
| `Silence.Window` | `20ms` | Volume-detection window |
| `Silence.MinSpeech` | `20ms` | Minimum retained speech interval |
| `Silence.Thresholds` | `nil` | Custom candidate thresholds; automatic thresholds when empty |
| `Silence.ThresholdFloor` / `ThresholdCeil` | `-60` / `-10` dB | Automatic threshold range |
| `FixedTrim.SliceLength` | `5s` | Fixed-slice length |
| `FixedTrim.Workers` | `16` | Fixed-slice concurrency |
| `FixedTrim.MinSegmentLength` | `10ms` | Minimum valid trimmed-slice length |
| `FixedTrim.TempDir` | empty | Temporary directory; created automatically when empty |
| `Segments.Workers` | `0` | Segment export/encoding concurrency; `0` means automatic |
| `Segments.MaxLength` | `175s` | Maximum ASR segment duration |
| `Segments.OutputSampleRate` | `16000` | Output sample rate |
| `Segments.OutputFormat` / `OutputCodec` | `ogg` / `libopus` | Output container and codec |
| `Segments.OutputBitrate` | `32k` | Output bitrate |
| `Segments.OutputSampleFormat` | `s16` | Output sample format/bit depth |
| `Segments.OutDir` | empty | Defaults to `out_segments` next to the input |
| `Segments.KeepTempWAV` | `true` | Keeps the intermediate WAV path in `Segment.TempWAV` |
| `Segments.PreserveInternalSilence` | `true` | Preserves internal silence when exporting segments |
| `Libav.CodecThreads` | `0` | Decoder/encoder threads per libav pipeline; uses libav defaults when `0` |

## Go library

### Import

```go
import smartaudio "github.com/Joey-Kot/ASR-Audio-Preprocess"
```

The production backend requires the `libav` build tag. Without `-tags libav`, `NewProcessor()` returns `ErrNoBackend`, unless a custom backend is injected with `WithBackend`.

### Complete workflow

```go
ctx := context.Background()

cfg := smartaudio.DefaultConfig()
cfg.Silence.MinSilence = 700 * time.Millisecond
cfg.Silence.Padding = 100 * time.Millisecond
cfg.FixedTrim.SliceLength = 5 * time.Second
cfg.FixedTrim.Workers = 16
cfg.Segments.MaxLength = 3 * time.Minute
cfg.Segments.OutputSampleRate = 16000
cfg.Segments.OutputSampleFormat = "s16"
cfg.Segments.OutDir = "/tmp/segments"
cfg.Segments.KeepTempWAV = smartaudio.Bool(true)
cfg.Segments.PreserveInternalSilence = smartaudio.Bool(true)
cfg.Libav.CodecThreads = 0

p, err := smartaudio.NewProcessor(smartaudio.WithConfig(cfg))
if err != nil {
	log.Fatal(err)
}

convertInfo, err := p.PreconvertToWAV(ctx, "/tmp/input.mp3", "/tmp/input.wav", 16000)
if err != nil {
	log.Fatal(err)
}

merged, trimInfo, err := p.RemoveSilenceByFixedSlicesAndMerge(ctx, "/tmp/input.wav", "/tmp/input_merged.wav")
if err != nil {
	log.Fatal(err)
}

segments, splitInfo, err := p.SplitWAVBySilenceGroups(ctx, merged)
if err != nil {
	log.Fatal(err)
}

log.Printf("input duration: %s", convertInfo.InputDuration)
log.Printf("fixed slices: total=%d ok=%d skipped=%d", trimInfo.FixedSliceCount, trimInfo.FixedSliceSucceeded, trimInfo.FixedSliceSkipped)
log.Printf("segments: total=%d files=%v", splitInfo.SegmentCount, splitInfo.OutputFiles)
```

### Main methods

```go
func (p *Processor) PreconvertToWAV(
	ctx context.Context, inputPath, wavPath string, sampleRate int,
) (ProcessingInfo, error)

func (p *Processor) TrimLongSilencesFromWAV(
	ctx context.Context, wavPath, outWAVPath string,
) (string, ProcessingInfo, error)

func (p *Processor) RemoveSilenceByFixedSlicesAndMerge(
	ctx context.Context, wavPath, outMergedWAV string,
) (string, ProcessingInfo, error)

func (p *Processor) SplitWAVBySilenceGroups(
	ctx context.Context, wavPath string,
) ([]Segment, ProcessingInfo, error)
```

`PreconvertToWAV` converts input audio to WAV. When `sampleRate <= 0`, it uses the configured sample rate.

`TrimLongSilencesFromWAV` trims long silence from one WAV and returns the final path and processing information. If there is nothing to trim, it returns the original `wavPath`.

`RemoveSilenceByFixedSlicesAndMerge` divides a WAV into fixed-length slices, trims long silence in parallel, and merges valid slices in their original order.

`SplitWAVBySilenceGroups` creates ordered `[]Segment` values according to silence intervals and `Segments.MaxLength`.

### Return structures

```go
type ProcessingInfo struct {
	InputPath, OutputPath                         string
	InputDuration, OutputDuration                 time.Duration
	DetectedEffectiveDuration, EffectiveDuration  time.Duration
	DetectedSilenceCount, DetectedSpeechIntervalCount int
	FixedSliceCount, FixedSliceSucceeded, FixedSliceSkipped int
	SegmentGroupCount, SegmentCount, SegmentSkipped int
	OutputFiles []string
}

type Segment struct {
	Index int
	File, TempWAV string
	Start, End, Cut, Duration time.Duration
	Intervals []Interval
	SourceWAV, SourcePath string
}
```

`ProcessingInfo` contains input/output and effective durations, detected silence and speech counts, fixed-slice totals, ASR segment totals, and the output-file list. `Segment.File` is the final file path that can be submitted to an ASR endpoint.

The default configuration prefers OGG/Opus. If default OGG/Opus encoding fails, the result falls back to the intermediate WAV path. When the output format, codec, or bitrate is explicitly configured, encoding failures are returned instead of silently falling back to WAV.

### Configuration

```go
cfg := smartaudio.DefaultConfig()
cfg.Silence.MinSilence = 700 * time.Millisecond
cfg.Silence.Padding = 100 * time.Millisecond
cfg.Silence.ThresholdDB = nil
cfg.Silence.Thresholds = []float64{-35, -40, -45}

cfg.FixedTrim.SliceLength = 5 * time.Second
cfg.FixedTrim.Workers = 16
cfg.FixedTrim.TempDir = "/tmp/smartaudio-work"

cfg.Segments.Workers = 0
cfg.Segments.MaxLength = 3 * time.Minute
cfg.Segments.OutputSampleRate = 16000
cfg.Segments.OutputFormat = "ogg"
cfg.Segments.OutputCodec = "libopus"
cfg.Segments.OutputBitrate = "32k"
cfg.Segments.OutputSampleFormat = "s16"
cfg.Segments.OutDir = "/tmp/segments"
cfg.Segments.KeepTempWAV = smartaudio.Bool(true)
cfg.Segments.PreserveInternalSilence = smartaudio.Bool(true)
cfg.Libav.CodecThreads = 0

p, err := smartaudio.NewProcessor(smartaudio.WithConfig(cfg))
```

`Libav.CodecThreads` is passed to the decoder and encoder of every built-in libav pipeline. With the default `0`, FFmpeg/libav controls its own threads. When setting a value above `0`, account for `FixedTrim.Workers` and `Segments.Workers` as well to avoid excessive concurrency.

Configure ASR output for your endpoint. The output is always mono:

```go
cfg.Segments.OutputFormat = "m4a"
cfg.Segments.OutputCodec = "aac"
cfg.Segments.OutputBitrate = "64k"
cfg.Segments.OutputSampleRate = 16000
cfg.Segments.OutputSampleFormat = "s16"
```

| `OutputFormat` | Default codec | Extension | Description |
| --- | --- | --- | --- |
| `ogg` / `opus` | `libopus` | `.ogg` | Default output; default bitrate `32k` |
| `wav` | `pcm_s16le` | `.wav` | Uncompressed PCM; bitrate ignored |
| `flac` | `flac` | `.flac` | Lossless compression; bitrate ignored |
| `aac` / `adts` | `aac` | `.aac` | ADTS AAC |
| `m4a` / `mp4` | `aac` | `.m4a` | MP4/M4A AAC |

When only `OutputFormat` is configured, the library selects the codec in this table. `OutputSampleFormat` supports `s16`, `s24`, `s32`, and `f32`; it is primarily useful for WAV/PCM. `OutputBitrate` applies only to `libopus`, `opus`, and `aac`.

Convenience options are also available:

```go
p, err := smartaudio.NewProcessor(
	smartaudio.WithMinSilence(700*time.Millisecond),
	smartaudio.WithSilencePadding(100*time.Millisecond),
	smartaudio.WithFixedSliceLength(5*time.Second),
	smartaudio.WithFixedSliceWorkers(16),
	smartaudio.WithMaxSegmentLength(3*time.Minute),
	smartaudio.WithLibavCodecThreads(0),
)
```

## CLI

The CLI at `cmd/smartaudio` is a thin entry point for the library: it calls the same `Processor` API and does not reimplement audio processing.

### Build

```bash
./scripts/bootstrap-static-audio-deps.sh

export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"
export PKG_CONFIG="pkg-config --static"

CGO_ENABLED=1 \
go build -tags libav -trimpath -ldflags="-s -w" -o smartaudio ./cmd/smartaudio
```

For stronger static linking, add appropriate external-linker flags:

```bash
CGO_ENABLED=1 \
go build -tags libav -trimpath \
  -ldflags="-s -w -linkmode external -extldflags '-static'" \
  -o smartaudio ./cmd/smartaudio
```

### Arguments

All arguments use the `--xx-xx value` form.

| Argument | Default | Description |
| --- | --- | --- |
| `--mode` | `process` | `process`, `preconvert`, `trim`, `fixed-trim`, or `split` |
| `--input` | empty | Required input audio path |
| `--output` | empty | Output path for `preconvert`, `trim`, or `fixed-trim`; merged WAV path for `process` |
| `--wav` | empty | Intermediate WAV path in `process` mode |
| `--work-dir` | temporary directory | Working and fixed-slice temporary directory |
| `--out-dir` | process: random-ID directory beside input; split: `out_segments` beside input | ASR segment output directory |
| `--output-sample-rate` | `16000` | Output sample rate |
| `--output-sample-format` | `s16` | `s16`, `s24`, `s32`, or `f32` |
| `--output-format` | `ogg` | `ogg`, `wav`, `flac`, `aac`, or `m4a` |
| `--output-codec` | `libopus` | ASR segment output codec |
| `--output-bitrate` | `32k` | ASR segment output bitrate |
| `--min-silence` / `--silence-padding` | `700ms` / `100ms` | Silence detection settings |
| `--fixed-slice-length` / `--fixed-slice-workers` | `5s` / `16` | Fixed-slice settings |
| `--segment-workers` | `0` | Export/encoding concurrency; automatic when `0` |
| `--libav-codec-threads` | `0` | Threads per libav pipeline; uses libav defaults when `0` |
| `--max-segment-length` | `175s` | Maximum ASR segment duration |
| `--keep-temp-wav` / `--preserve-internal-silence` | configuration default | `true` or `false` |

### Complete processing

```bash
./smartaudio \
  --mode process \
  --input /tmp/input.mp3 \
  --out-dir /tmp/segments \
  --max-segment-length 3m \
  --fixed-slice-workers 16 \
  --segment-workers 0 \
  --libav-codec-threads 0
```

To export M4A/AAC:

```bash
./smartaudio --mode process --input /tmp/input.mp3 --out-dir /tmp/segments \
  --output-format m4a --output-codec aac --output-bitrate 64k --output-sample-rate 16000
```

To export 16 kHz, 24-bit WAV:

```bash
./smartaudio --mode process --input /tmp/input.mp3 --out-dir /tmp/segments \
  --output-format wav --output-sample-rate 16000 --output-sample-format s24
```

```text
process: input audio -> WAV -> concurrent fixed-slice silence trimming -> merged WAV -> ASR segments
```

### Individual steps

```bash
./smartaudio --mode preconvert --input /tmp/input.mp3 --output /tmp/input.wav
./smartaudio --mode trim --input /tmp/input.wav --output /tmp/input_trimmed.wav
./smartaudio --mode fixed-trim --input /tmp/input.wav --output /tmp/input_merged.wav
./smartaudio --mode split --input /tmp/input_merged.wav --out-dir /tmp/segments
```

### CLI output

On success, the CLI writes JSON to stdout. On error, it writes JSON to stderr and exits non-zero. Duration fields include nanoseconds, a seconds string, and Go-style human-readable text.

## Static FFmpeg/libav dependencies

Use the provided script to download and statically build Opus and FFmpeg:

```bash
./scripts/bootstrap-static-audio-deps.sh
```

The default output directory is `third_party/ffmpeg-audio`. After it completes:

```bash
export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"
export PKG_CONFIG="pkg-config --static"
```

The script uses the official FFmpeg `8.1.2` release. It verifies the FFmpeg release PGP-key fingerprint and archive signature; the Opus archive is verified with SHA256.

The FFmpeg build retains only required audio components: `libavcodec`, `libavformat`, `libavfilter`, `libswresample`, common audio formats, required audio filters, and `libopus`, PCM, FLAC, and AAC encoders. Programs, documentation, networking, autodetection, video, subtitles, and unrelated components are disabled.

Release binaries use the same CLI interface and each archive contains:

```text
smartaudio / smartaudio.exe
LICENSE
NOTICE
THIRD_PARTY_NOTICES.md
THIRD_PARTY_LICENSES/
```

## Tests

```bash
# Regular tests
go test ./...

# Tests with the libav backend
export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"
go test -tags libav ./...

# Build the CLI
go build -tags libav -o smartaudio ./cmd/smartaudio
```

## Logging policy

The library does not write processing logs to stdout or stderr. Callers should log the returned `ProcessingInfo` and `Segment` values themselves.

The CLI always writes JSON:

- Success: stdout
- Failure: stderr

## License

This project is licensed under the GNU General Public License v3.0 or later. See [LICENSE](LICENSE). Complete third-party notices and license texts are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and [`THIRD_PARTY_LICENSES/`](THIRD_PARTY_LICENSES/).
