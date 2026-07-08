// Copyright (C) 2026 Joey Kot <joey.kot.x@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed WITHOUT ANY WARRANTY; without even the
// implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.
// See <https://www.gnu.org/licenses/> for more details.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	smartaudio "github.com/Joey-Kot/ASR-Audio-Preprocess"
)

type cliOptions struct {
	mode                    string
	input                   string
	output                  string
	wav                     string
	workDir                 string
	outDir                  string
	outputSampleRate        int
	outputFormat            string
	outputCodec             string
	outputBitrate           string
	outputSampleFormat      string
	minSilence              time.Duration
	silencePadding          time.Duration
	fixedSliceLength        time.Duration
	fixedSliceWorkers       int
	maxSegmentLength        time.Duration
	keepTempWAV             string
	preserveInternalSilence string
}

type cliResult struct {
	Mode       string              `json:"mode"`
	OutputPath string              `json:"output_path,omitempty"`
	Info       *processingInfoJSON `json:"info,omitempty"`
	Segments   []segmentJSON       `json:"segments,omitempty"`
	Steps      *stepInfoJSON       `json:"steps,omitempty"`
}

type stepInfoJSON struct {
	Preconvert *processingInfoJSON `json:"preconvert,omitempty"`
	FixedTrim  *processingInfoJSON `json:"fixed_trim,omitempty"`
	Split      *processingInfoJSON `json:"split,omitempty"`
}

type processingInfoJSON struct {
	InputPath  string `json:"input_path,omitempty"`
	OutputPath string `json:"output_path,omitempty"`

	InputDuration             durationJSON `json:"input_duration"`
	OutputDuration            durationJSON `json:"output_duration"`
	DetectedEffectiveDuration durationJSON `json:"detected_effective_duration"`
	EffectiveDuration         durationJSON `json:"effective_duration"`

	DetectedSilenceCount        int `json:"detected_silence_count"`
	DetectedSpeechIntervalCount int `json:"detected_speech_interval_count"`

	FixedSliceCount     int `json:"fixed_slice_count"`
	FixedSliceSucceeded int `json:"fixed_slice_succeeded"`
	FixedSliceSkipped   int `json:"fixed_slice_skipped"`

	SegmentGroupCount int      `json:"segment_group_count"`
	SegmentCount      int      `json:"segment_count"`
	SegmentSkipped    int      `json:"segment_skipped"`
	OutputFiles       []string `json:"output_files,omitempty"`
}

type segmentJSON struct {
	Index      int            `json:"index"`
	File       string         `json:"file"`
	TempWAV    string         `json:"temp_wav,omitempty"`
	Start      durationJSON   `json:"start"`
	End        durationJSON   `json:"end"`
	Cut        durationJSON   `json:"cut"`
	Duration   durationJSON   `json:"duration"`
	Intervals  []intervalJSON `json:"intervals,omitempty"`
	SourceWAV  string         `json:"source_wav,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
}

type intervalJSON struct {
	Start    durationJSON `json:"start"`
	End      durationJSON `json:"end"`
	Duration durationJSON `json:"duration"`
}

type durationJSON struct {
	Nanoseconds int64  `json:"nanoseconds"`
	Seconds     string `json:"seconds"`
	Human       string `json:"human"`
}

func main() {
	opts := parseFlags()
	if err := run(context.Background(), opts); err != nil {
		writeError(err)
		os.Exit(1)
	}
}

func parseFlags() cliOptions {
	opts := cliOptions{}
	flag.StringVar(&opts.mode, "mode", "process", "operation: process, preconvert, trim, fixed-trim, split")
	flag.StringVar(&opts.input, "input", "", "input audio path")
	flag.StringVar(&opts.output, "output", "", "output path for preconvert, trim, fixed-trim, or merged WAV in process mode")
	flag.StringVar(&opts.wav, "wav", "", "intermediate WAV path for process mode")
	flag.StringVar(&opts.workDir, "work-dir", "", "working directory for process mode and fixed slice temp files")
	flag.StringVar(&opts.outDir, "out-dir", "", "segment output directory")
	flag.IntVar(&opts.outputSampleRate, "output-sample-rate", smartaudio.DefaultOutputSampleRate, "segment output sample rate")
	flag.StringVar(&opts.outputFormat, "output-format", smartaudio.DefaultOutputFormat, "segment output container format, for example ogg, wav, flac, aac, or m4a")
	flag.StringVar(&opts.outputCodec, "output-codec", smartaudio.DefaultOutputCodec, "segment output encoder, for example libopus, pcm_s16le, pcm_s24le, pcm_s32le, pcm_f32le, flac, or aac")
	flag.StringVar(&opts.outputBitrate, "output-bitrate", smartaudio.DefaultOutputBitrate, "segment output bitrate, for example 32k or 64k")
	flag.StringVar(&opts.outputSampleFormat, "output-sample-format", smartaudio.DefaultOutputSampleFormat, "segment output sample format, for example s16, s24, s32, or f32")
	flag.DurationVar(&opts.minSilence, "min-silence", smartaudio.DefaultSilentInterval, "minimum silence duration, for example 700ms")
	flag.DurationVar(&opts.silencePadding, "silence-padding", smartaudio.DefaultPadding, "padding around retained speech, for example 100ms")
	flag.DurationVar(&opts.fixedSliceLength, "fixed-slice-length", smartaudio.DefaultFixedSliceLength, "fixed slice length, for example 5s")
	flag.IntVar(&opts.fixedSliceWorkers, "fixed-slice-workers", smartaudio.DefaultFixedSliceWorkers, "fixed slice worker count")
	flag.DurationVar(&opts.maxSegmentLength, "max-segment-length", smartaudio.DefaultMaxSegmentLength, "maximum ASR segment span, for example 3m")
	flag.StringVar(&opts.keepTempWAV, "keep-temp-wav", "", "whether split mode keeps temp WAV paths in results: true or false")
	flag.StringVar(&opts.preserveInternalSilence, "preserve-internal-silence", "", "whether split mode preserves internal silence: true or false")
	flag.Parse()
	return opts
}

func run(ctx context.Context, opts cliOptions) error {
	if opts.input == "" {
		return errors.New("--input is required")
	}
	cfg, err := buildConfig(opts)
	if err != nil {
		return err
	}
	p, err := smartaudio.NewProcessor(smartaudio.WithConfig(cfg))
	if err != nil {
		return err
	}
	switch opts.mode {
	case "preconvert":
		return runPreconvert(ctx, p, opts)
	case "trim":
		return runTrim(ctx, p, opts)
	case "fixed-trim":
		return runFixedTrim(ctx, p, opts)
	case "split":
		return runSplit(ctx, p, opts)
	case "process":
		return runProcess(ctx, p, opts)
	default:
		return fmt.Errorf("unsupported --mode %q", opts.mode)
	}
}

func buildConfig(opts cliOptions) (smartaudio.Config, error) {
	cfg := smartaudio.DefaultConfig()
	cfg.Silence.MinSilence = opts.minSilence
	cfg.Silence.Padding = opts.silencePadding
	cfg.FixedTrim.SliceLength = opts.fixedSliceLength
	cfg.FixedTrim.Workers = opts.fixedSliceWorkers
	cfg.Segments.MaxLength = opts.maxSegmentLength
	cfg.Segments.OutputSampleRate = opts.outputSampleRate
	cfg.Segments.OutputFormat = opts.outputFormat
	cfg.Segments.OutputCodec = opts.outputCodec
	cfg.Segments.OutputBitrate = opts.outputBitrate
	cfg.Segments.OutputSampleFormat = opts.outputSampleFormat
	cfg.Segments.OutDir = opts.outDir
	if opts.workDir != "" {
		cfg.FixedTrim.TempDir = opts.workDir
	}
	keepTempWAV, err := parseOptionalBool(opts.keepTempWAV)
	if err != nil {
		return cfg, fmt.Errorf("--keep-temp-wav: %w", err)
	}
	if keepTempWAV != nil {
		cfg.Segments.KeepTempWAV = keepTempWAV
	}
	preserveInternalSilence, err := parseOptionalBool(opts.preserveInternalSilence)
	if err != nil {
		return cfg, fmt.Errorf("--preserve-internal-silence: %w", err)
	}
	if preserveInternalSilence != nil {
		cfg.Segments.PreserveInternalSilence = preserveInternalSilence
	}
	return cfg, nil
}

func runPreconvert(ctx context.Context, p *smartaudio.Processor, opts cliOptions) error {
	if opts.output == "" {
		return errors.New("--output is required for --mode preconvert")
	}
	info, err := p.PreconvertToWAV(ctx, opts.input, opts.output, opts.outputSampleRate)
	if err != nil {
		return err
	}
	return writeJSON(cliResult{
		Mode:       opts.mode,
		OutputPath: opts.output,
		Info:       infoToJSON(info),
	})
}

func runTrim(ctx context.Context, p *smartaudio.Processor, opts cliOptions) error {
	output, info, err := p.TrimLongSilencesFromWAV(ctx, opts.input, opts.output)
	if err != nil {
		return err
	}
	return writeJSON(cliResult{
		Mode:       opts.mode,
		OutputPath: output,
		Info:       infoToJSON(info),
	})
}

func runFixedTrim(ctx context.Context, p *smartaudio.Processor, opts cliOptions) error {
	output, info, err := p.RemoveSilenceByFixedSlicesAndMerge(ctx, opts.input, opts.output)
	if err != nil {
		return err
	}
	return writeJSON(cliResult{
		Mode:       opts.mode,
		OutputPath: output,
		Info:       infoToJSON(info),
	})
}

func runSplit(ctx context.Context, p *smartaudio.Processor, opts cliOptions) error {
	segments, info, err := p.SplitWAVBySilenceGroups(ctx, opts.input)
	if err != nil {
		return err
	}
	return writeJSON(cliResult{
		Mode:       opts.mode,
		OutputPath: info.OutputPath,
		Info:       infoToJSON(info),
		Segments:   segmentsToJSON(segments),
	})
}

func runProcess(ctx context.Context, p *smartaudio.Processor, opts cliOptions) error {
	workDir := opts.workDir
	cleanupWorkDir := false
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "smartaudio-cli-*")
		if err != nil {
			return err
		}
		cleanupWorkDir = true
	}
	if cleanupWorkDir {
		defer os.RemoveAll(workDir)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}

	base := newCLIWorkFileStem()
	if p.Config().Segments.OutDir == "" {
		cfg := p.Config()
		cfg.Segments.OutDir = filepath.Join(filepath.Dir(opts.input), base+"_segments")
		var err error
		p, err = smartaudio.NewProcessor(smartaudio.WithConfig(cfg))
		if err != nil {
			return err
		}
	}
	wavPath := opts.wav
	if wavPath == "" {
		wavPath = filepath.Join(workDir, base+".wav")
	}
	mergedPath := opts.output
	if mergedPath == "" {
		mergedPath = filepath.Join(workDir, base+"_merged.wav")
	}

	var preInfo smartaudio.ProcessingInfo
	processInput := wavPath
	info, err := p.PreconvertToWAV(ctx, opts.input, processInput, opts.outputSampleRate)
	if err != nil {
		return err
	}
	preInfo = info

	merged, trimInfo, err := p.RemoveSilenceByFixedSlicesAndMerge(ctx, processInput, mergedPath)
	if err != nil {
		return err
	}
	if merged == "" {
		info := combineProcessInfo(opts.input, "", preInfo, trimInfo, smartaudio.ProcessingInfo{})
		info.DetectedEffectiveDuration = trimInfo.DetectedEffectiveDuration
		info.EffectiveDuration = trimInfo.EffectiveDuration
		info.OutputDuration = trimInfo.OutputDuration
		info.DetectedSpeechIntervalCount = trimInfo.DetectedSpeechIntervalCount
		return writeJSON(cliResult{
			Mode:       opts.mode,
			OutputPath: "",
			Info:       infoToJSON(info),
			Steps: &stepInfoJSON{
				Preconvert: infoToJSON(preInfo),
				FixedTrim:  infoToJSON(trimInfo),
				Split:      infoToJSON(smartaudio.ProcessingInfo{}),
			},
		})
	}
	segments, splitInfo, err := p.SplitWAVBySilenceGroups(ctx, merged)
	if err != nil {
		return err
	}
	info = combineProcessInfo(opts.input, splitInfo.OutputPath, preInfo, trimInfo, splitInfo)
	return writeJSON(cliResult{
		Mode:       opts.mode,
		OutputPath: splitInfo.OutputPath,
		Info:       infoToJSON(info),
		Segments:   segmentsToJSON(segments),
		Steps: &stepInfoJSON{
			Preconvert: infoToJSON(preInfo),
			FixedTrim:  infoToJSON(trimInfo),
			Split:      infoToJSON(splitInfo),
		},
	})
}

func combineProcessInfo(inputPath, outputPath string, preInfo, trimInfo, splitInfo smartaudio.ProcessingInfo) smartaudio.ProcessingInfo {
	return smartaudio.ProcessingInfo{
		InputPath:                   inputPath,
		OutputPath:                  outputPath,
		InputDuration:               preInfo.InputDuration,
		OutputDuration:              splitInfo.OutputDuration,
		DetectedEffectiveDuration:   splitInfo.DetectedEffectiveDuration,
		EffectiveDuration:           splitInfo.EffectiveDuration,
		DetectedSilenceCount:        trimInfo.DetectedSilenceCount + splitInfo.DetectedSilenceCount,
		DetectedSpeechIntervalCount: splitInfo.DetectedSpeechIntervalCount,
		FixedSliceCount:             trimInfo.FixedSliceCount,
		FixedSliceSucceeded:         trimInfo.FixedSliceSucceeded,
		FixedSliceSkipped:           trimInfo.FixedSliceSkipped,
		SegmentGroupCount:           splitInfo.SegmentGroupCount,
		SegmentCount:                splitInfo.SegmentCount,
		SegmentSkipped:              splitInfo.SegmentSkipped,
		OutputFiles:                 append([]string(nil), splitInfo.OutputFiles...),
	}
}

func parseOptionalBool(value string) (*bool, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, err
	}
	return smartaudio.Bool(parsed), nil
}

func newCLIWorkFileStem() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "sa_" + hex.EncodeToString(b[:])
	}
	return "sa_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

func writeJSON(result cliResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func writeError(err error) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
		"error": err.Error(),
	})
}

func infoToJSON(info smartaudio.ProcessingInfo) *processingInfoJSON {
	return &processingInfoJSON{
		InputPath:                   info.InputPath,
		OutputPath:                  info.OutputPath,
		InputDuration:               durationToJSON(info.InputDuration),
		OutputDuration:              durationToJSON(info.OutputDuration),
		DetectedEffectiveDuration:   durationToJSON(info.DetectedEffectiveDuration),
		EffectiveDuration:           durationToJSON(info.EffectiveDuration),
		DetectedSilenceCount:        info.DetectedSilenceCount,
		DetectedSpeechIntervalCount: info.DetectedSpeechIntervalCount,
		FixedSliceCount:             info.FixedSliceCount,
		FixedSliceSucceeded:         info.FixedSliceSucceeded,
		FixedSliceSkipped:           info.FixedSliceSkipped,
		SegmentGroupCount:           info.SegmentGroupCount,
		SegmentCount:                info.SegmentCount,
		SegmentSkipped:              info.SegmentSkipped,
		OutputFiles:                 append([]string(nil), info.OutputFiles...),
	}
}

func segmentsToJSON(segments []smartaudio.Segment) []segmentJSON {
	out := make([]segmentJSON, 0, len(segments))
	for _, segment := range segments {
		out = append(out, segmentJSON{
			Index:      segment.Index,
			File:       segment.File,
			TempWAV:    segment.TempWAV,
			Start:      durationToJSON(segment.Start),
			End:        durationToJSON(segment.End),
			Cut:        durationToJSON(segment.Cut),
			Duration:   durationToJSON(segment.Duration),
			Intervals:  intervalsToJSON(segment.Intervals),
			SourceWAV:  segment.SourceWAV,
			SourcePath: segment.SourcePath,
		})
	}
	return out
}

func intervalsToJSON(intervals []smartaudio.Interval) []intervalJSON {
	out := make([]intervalJSON, 0, len(intervals))
	for _, interval := range intervals {
		out = append(out, intervalJSON{
			Start:    durationToJSON(interval.Start),
			End:      durationToJSON(interval.End),
			Duration: durationToJSON(interval.Duration()),
		})
	}
	return out
}

func durationToJSON(duration time.Duration) durationJSON {
	return durationJSON{
		Nanoseconds: int64(duration),
		Seconds:     strconv.FormatFloat(duration.Seconds(), 'f', 6, 64),
		Human:       duration.String(),
	}
}
