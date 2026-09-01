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
	"path/filepath"
	"testing"
	"time"

	smartaudio "github.com/Joey-Kot/ASR-Audio-Preprocess"
)

type splitTestBackend struct {
	transcodeInput  string
	transcodeOutput string
	transcodeRate   int
	exportInputs    []string
	encodeCalls     []encodeAudioCall
}

type encodeAudioCall struct {
	outputPath   string
	sampleRate   int
	format       string
	codec        string
	bitrate      string
	sampleFormat string
}

func (*splitTestBackend) ProbeDuration(context.Context, string, smartaudio.ProbeOrder) (time.Duration, error) {
	return 10 * time.Second, nil
}

func (*splitTestBackend) VolumeDetect(context.Context, string) (smartaudio.VolumeStats, error) {
	return smartaudio.VolumeStats{MeanDB: -30, HasMean: true, Valid: true}, nil
}

func (*splitTestBackend) SilenceDetect(context.Context, string, float64, time.Duration) ([]smartaudio.Interval, error) {
	return []smartaudio.Interval{{Start: 4 * time.Second, End: 5 * time.Second}}, nil
}

func (b *splitTestBackend) TranscodeToWAV(_ context.Context, inputPath, wavPath string, sampleRate int) error {
	b.transcodeInput = inputPath
	b.transcodeOutput = wavPath
	b.transcodeRate = sampleRate
	return nil
}

func (*splitTestBackend) SplitWAVFixed(context.Context, string, string, string, time.Duration, int) ([]string, error) {
	return nil, nil
}

func (b *splitTestBackend) ExportWAV(_ context.Context, inputPath, _ string, _ time.Duration, _ time.Duration, _ int) error {
	b.exportInputs = append(b.exportInputs, inputPath)
	return nil
}

func (*splitTestBackend) RenderIntervalsToWAV(context.Context, string, string, []smartaudio.Interval, int) error {
	return nil
}

func (*splitTestBackend) ConcatWAV(context.Context, []string, string) error {
	return nil
}

func (b *splitTestBackend) EncodeAudio(_ context.Context, _ string, outputPath string, sampleRate int, format, codec, bitrate, sampleFormat string) error {
	b.encodeCalls = append(b.encodeCalls, encodeAudioCall{
		outputPath:   outputPath,
		sampleRate:   sampleRate,
		format:       format,
		codec:        codec,
		bitrate:      bitrate,
		sampleFormat: sampleFormat,
	})
	return nil
}

func (*splitTestBackend) EncodeOpus(context.Context, string, string, int, string) error {
	return nil
}

func TestRunSplitPreconvertsThenExportsConfiguredSegments(t *testing.T) {
	workDir := t.TempDir()
	outDir := t.TempDir()
	backend := &splitTestBackend{}
	cfg := smartaudio.DefaultConfig()
	cfg.Segments.Workers = 1
	cfg.Segments.OutDir = outDir
	cfg.Segments.OutputFormat = "m4a"
	cfg.Segments.OutputCodec = "aac"
	cfg.Segments.OutputBitrate = "64k"
	cfg.Segments.OutputSampleRate = 24000
	cfg.Segments.OutputSampleFormat = "s16"
	p, err := smartaudio.NewProcessor(smartaudio.WithBackend(backend), smartaudio.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}

	err = runSplit(context.Background(), p, cliOptions{
		input:            "input.mp3",
		workDir:          workDir,
		outputSampleRate: 24000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend.transcodeInput != "input.mp3" {
		t.Fatalf("transcode input=%q want input.mp3", backend.transcodeInput)
	}
	if filepath.Dir(backend.transcodeOutput) != workDir || filepath.Ext(backend.transcodeOutput) != ".wav" {
		t.Fatalf("intermediate WAV path=%q want a WAV under %q", backend.transcodeOutput, workDir)
	}
	if backend.transcodeRate != 24000 {
		t.Fatalf("transcode sample rate=%d want 24000", backend.transcodeRate)
	}
	if len(backend.exportInputs) == 0 || backend.exportInputs[0] != backend.transcodeOutput {
		t.Fatalf("split input=%#v want preconverted WAV %q", backend.exportInputs, backend.transcodeOutput)
	}
	if len(backend.encodeCalls) != 1 {
		t.Fatalf("encode calls=%d want 1", len(backend.encodeCalls))
	}
	call := backend.encodeCalls[0]
	if filepath.Ext(call.outputPath) != ".m4a" || call.format != "mp4" || call.codec != "aac" || call.bitrate != "64k" || call.sampleRate != 24000 || call.sampleFormat != "s16" {
		t.Fatalf("encode call=%+v does not match configured M4A/AAC output", call)
	}
}

func TestRemapTemporarySplitSources(t *testing.T) {
	segments := []smartaudio.Segment{{
		SourceWAV:  "/tmp/smartaudio-cli/input.wav",
		SourcePath: "/tmp/smartaudio-cli/input.wav",
	}}
	remapTemporarySplitSources(segments, "/tmp/input.mp3", true)
	if segments[0].SourceWAV != "" || segments[0].SourcePath != "/tmp/input.mp3" {
		t.Fatalf("temporary source paths=%+v want empty SourceWAV and original input path", segments[0])
	}

	segments = []smartaudio.Segment{{
		SourceWAV:  "/tmp/work/input.wav",
		SourcePath: "/tmp/work/input.wav",
	}}
	remapTemporarySplitSources(segments, "/tmp/input.mp3", false)
	if segments[0].SourceWAV != "/tmp/work/input.wav" || segments[0].SourcePath != "/tmp/work/input.wav" {
		t.Fatalf("persistent source paths=%+v should remain unchanged", segments[0])
	}
}
