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

package smartaudio

import (
	"context"
	"errors"
	"time"
)

var ErrNoBackend = errors.New("smartaudio: no in-process audio backend configured; build with -tags libav or pass a backend")

type Backend interface {
	ProbeDuration(ctx context.Context, path string, order ProbeOrder) (time.Duration, error)
	VolumeDetect(ctx context.Context, path string) (VolumeStats, error)
	SilenceDetect(ctx context.Context, path string, noiseDB float64, minSilence time.Duration) ([]Interval, error)
	TranscodeToWAV(ctx context.Context, inputPath, wavPath string, sampleRate int) error
	SplitWAVFixed(ctx context.Context, wavPath, outDir, filenamePrefix string, sliceLength time.Duration, sampleRate int) ([]string, error)
	ExportWAV(ctx context.Context, inputPath, wavPath string, start, end time.Duration, sampleRate int) error
	RenderIntervalsToWAV(ctx context.Context, inputPath, outWAVPath string, intervals []Interval, sampleRate int) error
	ConcatWAV(ctx context.Context, wavPaths []string, outPath string) error
	EncodeAudio(ctx context.Context, wavPath, outPath string, sampleRate int, format, codec, bitrate string) error
	EncodeOpus(ctx context.Context, wavPath, oggPath string, sampleRate int, bitrate string) error
}

type Option func(*Processor)

func WithConfig(cfg Config) Option {
	return func(p *Processor) {
		p.cfg = cfg.normalized()
	}
}

func WithBackend(backend Backend) Option {
	return func(p *Processor) {
		p.backend = backend
	}
}

func WithSilencePadding(padding time.Duration) Option {
	return func(p *Processor) {
		p.cfg.Silence.Padding = padding
	}
}

func WithMinSilence(minSilence time.Duration) Option {
	return func(p *Processor) {
		p.cfg.Silence.MinSilence = minSilence
	}
}

func WithFixedSliceLength(sliceLength time.Duration) Option {
	return func(p *Processor) {
		p.cfg.FixedTrim.SliceLength = sliceLength
	}
}

func WithFixedSliceWorkers(workers int) Option {
	return func(p *Processor) {
		if workers > 0 {
			p.cfg.FixedTrim.Workers = workers
		}
	}
}

func WithMaxSegmentLength(maxLength time.Duration) Option {
	return func(p *Processor) {
		if maxLength > 0 {
			p.cfg.Segments.MaxLength = maxLength
		}
	}
}
