# ASR-Audio-Preprocess

ASR-Audio-Preprocess 是一个 Go 音频预处理库，同时提供一个薄 CLI 入口。它把音频静音裁剪、固定分片并发处理、有序合并、按静音区间切分 ASR 请求分片这些核心流程抽取为可复用模块。

项目目标是让其他 Go 项目可以直接 `import` 使用，也让非 Go 项目可以通过预编译二进制调用同一套处理能力。

## 特性

- Go library 和 CLI 两种调用形态
- 进程内调用静态链接的 FFmpeg/libav，不调用外部 `ffmpeg`、`ffprobe`、`mediainfo` 子命令
- 音频转 WAV、时长探测、音量检测、静音检测、裁剪、分片、合并、Opus 编码全部由 libav 完成
- 固定长度切片并发裁剪，按原顺序合并
- 按静音区间生成适合 ASR 并发请求的分片
- 返回结构化处理信息，便于调用方记录日志
- GitHub Actions 自动构建 Linux / Windows 的 x86_64 和 arm64 CLI release

## 处理流程

完整处理链路如下：

```text
输入音频
  -> 转换为统一采样率 WAV
  -> 固定长度切片
  -> 并发裁剪每个切片中的长静音
  -> 按原顺序合并有效切片
  -> 按静音区间和最大请求长度切成 ASR 分片
  -> 输出 OGG/WAV 分片路径和处理信息
```

Go 负责 API、配置、并发调度、顺序组装和返回结构。音频领域的解码、滤镜、裁剪、复用、编码工作交给 FFmpeg/libav。

## 默认参数

| 参数 | 默认值 | 含义 |
| --- | --- | --- |
| `DefaultSampleRate` | `16000` | 输出采样率 |
| `DefaultSilentInterval` | `700ms` | 最小静音区间 |
| `DefaultPadding` | `100ms` | 保留语音区间两侧 padding |
| `DefaultFixedSliceLength` | `5s` | 固定切片长度 |
| `DefaultFixedSliceWorkers` | `16` | 固定切片并发数 |
| `DefaultMaxSegmentLength` | `175s` | ASR 分片最大时间跨度 |
| `DefaultOpusBitrate` | `32k` | OGG/Opus 输出码率 |
| `DefaultMinOutputSegmentLen` | `10ms` | 固定切片裁剪后的最小有效长度 |

## Go Library

### 引入

```go
import smartaudio "github.com/Joey-Kot/ASR-Audio-Preprocess"
```

生产后端需要使用 `libav` build tag。没有 `-tags libav` 时，`NewProcessor()` 会返回 `ErrNoBackend`，除非你通过 `WithBackend` 注入自己的后端实现。

### 完整流程

```go
package main

import (
	"context"
	"log"
	"time"

	smartaudio "github.com/Joey-Kot/ASR-Audio-Preprocess"
)

func main() {
	ctx := context.Background()

	cfg := smartaudio.DefaultConfig()
	cfg.Silence.MinSilence = 700 * time.Millisecond
	cfg.Silence.Padding = 100 * time.Millisecond
	cfg.FixedTrim.SliceLength = 5 * time.Second
	cfg.FixedTrim.Workers = 16
	cfg.Segments.MaxLength = 3 * time.Minute
	cfg.Segments.SampleRate = 16000
	cfg.Segments.OutDir = "/tmp/segments"
	cfg.Segments.KeepTempWAV = smartaudio.Bool(true)
	cfg.Segments.PreserveInternalSilence = smartaudio.Bool(true)

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

	for _, segment := range segments {
		log.Printf("segment %d: %s", segment.Index, segment.File)
	}
}
```

### 主要方法

```go
func (p *Processor) PreconvertToWAV(
	ctx context.Context,
	inputPath string,
	wavPath string,
	sampleRate int,
) (ProcessingInfo, error)
```

把输入音频转换为 WAV。`sampleRate <= 0` 时使用配置中的采样率。

```go
func (p *Processor) TrimLongSilencesFromWAV(
	ctx context.Context,
	wavPath string,
	outWAVPath string,
) (string, ProcessingInfo, error)
```

裁剪单个 WAV 中的长静音。返回最终 WAV 路径和处理信息。没有可裁剪内容时返回原始 `wavPath`。

```go
func (p *Processor) RemoveSilenceByFixedSlicesAndMerge(
	ctx context.Context,
	wavPath string,
	outMergedWAV string,
) (string, ProcessingInfo, error)
```

将 WAV 按固定长度切片，并发裁剪每个切片中的长静音，再按原顺序合并。返回合并后的 WAV 路径和处理信息。

```go
func (p *Processor) SplitWAVBySilenceGroups(
	ctx context.Context,
	wavPath string,
) ([]Segment, ProcessingInfo, error)
```

按静音区间和 `Segments.MaxLength` 切分 ASR 请求分片。返回有序 `[]Segment` 和处理信息。

### 返回结构

`ProcessingInfo` 用于调用方记录日志和监控：

```go
type ProcessingInfo struct {
	InputPath  string
	OutputPath string

	InputDuration             time.Duration
	OutputDuration            time.Duration
	DetectedEffectiveDuration time.Duration
	EffectiveDuration         time.Duration

	DetectedSilenceCount        int
	DetectedSpeechIntervalCount int

	FixedSliceCount     int
	FixedSliceSucceeded int
	FixedSliceSkipped   int

	SegmentGroupCount int
	SegmentCount      int
	SegmentSkipped    int
	OutputFiles       []string
}
```

字段含义：

- `InputDuration`：输入音频时长
- `OutputDuration`：输出文件或输出分片的总时长
- `DetectedEffectiveDuration`：检测到的有效语音总长
- `EffectiveDuration`：成功输出结果对应的有效长度
- `DetectedSilenceCount`：检测到的静音区间数量
- `DetectedSpeechIntervalCount`：反转静音区间后得到的有效语音区间数量
- `FixedSliceCount`：固定切片数量
- `FixedSliceSucceeded`：成功参与合并的固定切片数量
- `FixedSliceSkipped`：失败、过短或无效而跳过的固定切片数量
- `SegmentGroupCount`：候选 ASR 分片组数量
- `SegmentCount`：最终成功输出的 ASR 分片数量
- `SegmentSkipped`：导出失败而跳过的 ASR 分片数量
- `OutputFiles`：最终输出文件路径列表

`Segment` 表示一个 ASR 分片：

```go
type Segment struct {
	Index      int
	File       string
	TempWAV    string
	Start      time.Duration
	End        time.Duration
	Cut        time.Duration
	Duration   time.Duration
	Intervals  []Interval
	SourceWAV  string
	SourcePath string
}
```

`File` 优先是 OGG/Opus 文件路径；如果 OGG 编码失败，会回退为 WAV 文件路径。

### 配置

可以通过 `Config` 完整配置：

```go
cfg := smartaudio.DefaultConfig()

cfg.Silence.MinSilence = 700 * time.Millisecond
cfg.Silence.Padding = 100 * time.Millisecond
cfg.Silence.ThresholdDB = nil
cfg.Silence.Thresholds = []float64{-35, -40, -45}

cfg.FixedTrim.SliceLength = 5 * time.Second
cfg.FixedTrim.Workers = 16
cfg.FixedTrim.TempDir = "/tmp/smartaudio-work"

cfg.Segments.MaxLength = 3 * time.Minute
cfg.Segments.SampleRate = 16000
cfg.Segments.OpusBitrate = "32k"
cfg.Segments.OutDir = "/tmp/segments"
cfg.Segments.KeepTempWAV = smartaudio.Bool(true)
cfg.Segments.PreserveInternalSilence = smartaudio.Bool(true)

p, err := smartaudio.NewProcessor(smartaudio.WithConfig(cfg))
```

也可以使用少量便捷 option：

```go
p, err := smartaudio.NewProcessor(
	smartaudio.WithMinSilence(700*time.Millisecond),
	smartaudio.WithSilencePadding(100*time.Millisecond),
	smartaudio.WithFixedSliceLength(5*time.Second),
	smartaudio.WithFixedSliceWorkers(16),
	smartaudio.WithMaxSegmentLength(3*time.Minute),
)
```

## CLI

CLI 位于 `cmd/smartaudio`，是 library 的薄入口。它不重新实现音频处理逻辑，只调用同一套 `Processor` API。

### 构建

```bash
./scripts/bootstrap-static-audio-deps.sh

export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"
export PKG_CONFIG="pkg-config --static"

CGO_ENABLED=1 \
go build -tags libav -trimpath -ldflags="-s -w" -o smartaudio ./cmd/smartaudio
```

如需更强的静态链接，可按目标平台加入外部链接参数：

```bash
CGO_ENABLED=1 \
go build -tags libav -trimpath \
  -ldflags="-s -w -linkmode external -extldflags '-static'" \
  -o smartaudio ./cmd/smartaudio
```

### 参数

所有参数都使用 `--xx-xx value` 形态。

| 参数 | 默认值 | 含义 |
| --- | --- | --- |
| `--mode` | `process` | 操作模式：`process`、`preconvert`、`trim`、`fixed-trim`、`split` |
| `--input` | 空 | 输入音频路径，必填 |
| `--output` | 空 | 输出路径，用于 `preconvert`、`trim`、`fixed-trim`，也可作为 `process` 的合并 WAV 路径 |
| `--wav` | 空 | `process` 模式的中间 WAV 路径 |
| `--work-dir` | 临时目录 | 工作目录和固定切片临时目录 |
| `--out-dir` | 输入同目录下的 `out_segments` | ASR 分片输出目录 |
| `--sample-rate` | `16000` | 输出采样率 |
| `--opus-bitrate` | `32k` | OGG/Opus 码率 |
| `--min-silence` | `700ms` | 最小静音区间 |
| `--silence-padding` | `100ms` | 语音区间两侧 padding |
| `--fixed-slice-length` | `5s` | 固定切片长度 |
| `--fixed-slice-workers` | `16` | 固定切片并发数 |
| `--max-segment-length` | `175s` | ASR 分片最大时间跨度 |
| `--keep-temp-wav` | 配置默认值 | 是否在结果中保留中间 WAV 路径：`true` 或 `false` |
| `--preserve-internal-silence` | 配置默认值 | 分片导出时是否保留分片内部静音：`true` 或 `false` |

### 完整处理

```bash
./smartaudio \
  --mode process \
  --input /tmp/input.mp3 \
  --out-dir /tmp/segments \
  --max-segment-length 3m \
  --fixed-slice-workers 16
```

`process` 模式会执行：

```text
输入音频 -> WAV -> 固定切片并发静音裁剪 -> 合并 WAV -> ASR 分片
```

### 单步处理

```bash
./smartaudio --mode preconvert --input /tmp/input.mp3 --output /tmp/input.wav
```

```bash
./smartaudio --mode trim --input /tmp/input.wav --output /tmp/input_trimmed.wav
```

```bash
./smartaudio --mode fixed-trim --input /tmp/input.wav --output /tmp/input_merged.wav
```

```bash
./smartaudio --mode split --input /tmp/input_merged.wav --out-dir /tmp/segments
```

### CLI 输出

CLI 成功时向 stdout 输出 JSON。错误时向 stderr 输出 JSON，并返回非零退出码。

示例输出结构：

```json
{
  "mode": "process",
  "output_path": "/tmp/segments",
  "info": {
    "input_path": "/tmp/input.mp3",
    "output_path": "/tmp/segments",
    "input_duration": {
      "nanoseconds": 1800000000000,
      "seconds": "1800.000000",
      "human": "30m0s"
    },
    "output_duration": {
      "nanoseconds": 1200000000000,
      "seconds": "1200.000000",
      "human": "20m0s"
    },
    "fixed_slice_count": 360,
    "fixed_slice_succeeded": 340,
    "fixed_slice_skipped": 20,
    "segment_group_count": 7,
    "segment_count": 7,
    "segment_skipped": 0,
    "output_files": [
      "/tmp/segments/input_merged_part001.ogg",
      "/tmp/segments/input_merged_part002.ogg"
    ]
  },
  "segments": [],
  "steps": {
    "preconvert": {},
    "fixed_trim": {},
    "split": {}
  }
}
```

实际输出中的 `duration` 字段都会同时包含纳秒、秒字符串和 Go 风格可读文本，方便不同语言解析。

## 静态 FFmpeg/libav 依赖

项目提供了 `scripts/bootstrap-static-audio-deps.sh`，用于下载并静态构建 Opus 和 FFmpeg：

```bash
./scripts/bootstrap-static-audio-deps.sh
```

默认输出到：

```text
third_party/ffmpeg-audio
```

构建完成后设置：

```bash
export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"
export PKG_CONFIG="pkg-config --static"
```

FFmpeg 配置只保留音频处理所需能力，禁用了程序、文档、网络、自动探测、视频、字幕和无关组件。当前保留的核心能力包括：

- `libavcodec`
- `libavformat`
- `libavfilter`
- `libswresample`
- `wav`、`mp3`、`aac`、`mov`、`matroska`、`ogg`、`flac` 等常用输入
- `wav`、`ogg`、`segment` 输出
- `volumedetect`、`silencedetect`、`asplit`、`atrim`、`asetpts`、`concat`、`aresample` 等滤镜
- `libopus` 编码

## Release

仓库包含 GitHub Actions workflow：

```text
.github/workflows/release-latest.yml
```

触发条件：

- push 到 `main`
- push 到 `dev`
- 手动触发 `workflow_dispatch`

发布策略：

- 固定 tag 名：`Latest`
- 每次运行都会强制移动 `Latest` tag 到当前提交
- 删除旧的 `Latest` release
- 重新创建 release 并上传最新产物

发布产物：

```text
smartaudio-linux-amd64.tar.gz
smartaudio-linux-amd64.tar.gz.sha256
smartaudio-linux-arm64.tar.gz
smartaudio-linux-arm64.tar.gz.sha256
smartaudio-windows-amd64.zip
smartaudio-windows-amd64.zip.sha256
smartaudio-windows-arm64.zip
smartaudio-windows-arm64.zip.sha256
```

Release 二进制面向非 Go 项目使用，调用方式和本地构建的 CLI 一致。

每个压缩包内都会包含：

```text
smartaudio / smartaudio.exe
LICENSE
THIRD_PARTY_NOTICES.md
```

## 测试

普通测试：

```bash
go test ./...
```

带 libav 后端测试：

```bash
export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"

go test -tags libav ./...
```

构建 CLI：

```bash
export PKG_CONFIG_PATH="$PWD/third_party/ffmpeg-audio/lib/pkgconfig"

go build -tags libav -o smartaudio ./cmd/smartaudio
```

## 日志策略

库本身不主动向 stdout 或 stderr 输出处理日志。调用方应使用返回的 `ProcessingInfo` 和 `Segment` 自行记录日志。

CLI 则固定输出 JSON：

- 成功：stdout
- 失败：stderr

## 许可证

本项目使用 GNU General Public License v3.0 or later。详见 [LICENSE](LICENSE)。
