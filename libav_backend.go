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

//go:build libav

package smartaudio

import (
	"context"
	"time"

	"github.com/Joey-Kot/ASR-Audio-Preprocess/internal/libavshim"
)

type LibavBackend struct {
	codecThreads int
}

func defaultBackend() Backend {
	return LibavBackend{}
}

func NewLibavBackend() Backend {
	return LibavBackend{}
}

func (b LibavBackend) withConfig(cfg Config) Backend {
	b.codecThreads = cfg.Libav.CodecThreads
	return b
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (b LibavBackend) ProbeDuration(ctx context.Context, path string, order ProbeOrder) (time.Duration, error) {
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	return libavshim.ProbeDuration(ctx, path, order == ProbeMediaFirst, b.codecThreads)
}

func (b LibavBackend) VolumeDetect(ctx context.Context, path string) (VolumeStats, error) {
	if err := contextErr(ctx); err != nil {
		return VolumeStats{}, err
	}
	stats, err := libavshim.VolumeDetect(ctx, path, b.codecThreads)
	if err != nil {
		return VolumeStats{}, err
	}
	return VolumeStats{MeanDB: stats.MeanDB, MaxDB: stats.MaxDB, HasMean: true, HasMax: true, Valid: true}, nil
}

func (b LibavBackend) SilenceDetect(ctx context.Context, path string, noiseDB float64, minSilence time.Duration) ([]Interval, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	shimIntervals, err := libavshim.SilenceDetect(ctx, path, noiseDB, minSilence, b.codecThreads)
	if err != nil {
		return nil, err
	}
	if len(shimIntervals) == 0 {
		return nil, nil
	}
	out := make([]Interval, 0, len(shimIntervals))
	for _, iv := range shimIntervals {
		out = append(out, Interval{
			Start: iv.Start,
			End:   iv.End,
		})
	}
	return out, nil
}

func (b LibavBackend) TranscodeToWAV(ctx context.Context, inputPath, wavPath string, sampleRate int) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return libavshim.TranscodeToWAV(ctx, inputPath, wavPath, sampleRate, b.codecThreads)
}

func (b LibavBackend) SplitWAVFixed(ctx context.Context, wavPath, outDir, filenamePrefix string, sliceLength time.Duration, sampleRate int) ([]string, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return libavshim.SplitWAVFixed(ctx, wavPath, outDir, filenamePrefix, sliceLength, sampleRate, b.codecThreads)
}

func (b LibavBackend) ExportWAV(ctx context.Context, inputPath, wavPath string, start, end time.Duration, sampleRate int) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return libavshim.ExportWAV(ctx, inputPath, wavPath, start, end, sampleRate, b.codecThreads)
}

func (b LibavBackend) RenderIntervalsToWAV(ctx context.Context, inputPath, outWAVPath string, intervals []Interval, sampleRate int) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	shimIntervals := make([]libavshim.Interval, len(intervals))
	for i, iv := range intervals {
		shimIntervals[i] = libavshim.Interval{Start: iv.Start, End: iv.End}
	}
	return libavshim.RenderIntervalsToWAV(ctx, inputPath, outWAVPath, shimIntervals, sampleRate, b.codecThreads)
}

func (b LibavBackend) ConcatWAV(ctx context.Context, wavPaths []string, outPath string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return libavshim.ConcatWAV(ctx, wavPaths, outPath, b.codecThreads)
}

func (b LibavBackend) EncodeOpus(ctx context.Context, wavPath, oggPath string, sampleRate int, bitrate string) error {
	return b.EncodeAudio(ctx, wavPath, oggPath, sampleRate, "ogg", "libopus", bitrate, DefaultOutputSampleFormat)
}

func (b LibavBackend) EncodeAudio(ctx context.Context, wavPath, outPath string, sampleRate int, format, codec, bitrate, sampleFormat string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return libavshim.EncodeAudio(ctx, wavPath, outPath, sampleRate, format, codec, bitrate, sampleFormat, b.codecThreads)
}
