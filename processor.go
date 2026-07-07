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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Processor struct {
	cfg     Config
	backend Backend
}

func NewProcessor(opts ...Option) (*Processor, error) {
	p := &Processor{
		cfg:     DefaultConfig(),
		backend: defaultBackend(),
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.backend == nil {
		return nil, ErrNoBackend
	}
	return p, nil
}

func (p *Processor) Config() Config {
	return p.cfg
}

func (p *Processor) ProbeDuration(ctx context.Context, path string, order ProbeOrder) (time.Duration, error) {
	return p.backend.ProbeDuration(ctx, path, order)
}

func (p *Processor) PreconvertToWAV(ctx context.Context, inputPath, wavPath string, sampleRate int) (ProcessingInfo, error) {
	info := ProcessingInfo{
		InputPath:  inputPath,
		OutputPath: wavPath,
	}
	duration, err := p.backend.ProbeDuration(ctx, inputPath, ProbeMediaFirst)
	if err != nil {
		return info, err
	}
	info.InputDuration = duration
	if sampleRate <= 0 {
		sampleRate = p.cfg.Segments.SampleRate
	}
	if err := p.backend.TranscodeToWAV(ctx, inputPath, wavPath, sampleRate); err != nil {
		return info, err
	}
	if outputDuration, err := p.backend.ProbeDuration(ctx, wavPath, ProbeWAVFirst); err == nil {
		info.OutputDuration = outputDuration
	}
	return info, nil
}

func (p *Processor) TrimLongSilencesFromWAV(ctx context.Context, wavPath, outWAVPath string) (string, ProcessingInfo, error) {
	info := ProcessingInfo{InputPath: wavPath}
	duration, err := p.backend.ProbeDuration(ctx, wavPath, ProbeWAVFirst)
	if err != nil {
		return wavPath, info, err
	}
	info.InputDuration = duration
	silences, err := p.detectSilences(ctx, wavPath)
	if err != nil {
		return wavPath, info, err
	}
	info.DetectedSilenceCount = len(silences)
	intervals := InvertAndExpand(silences, duration, p.cfg.Silence.Padding, p.cfg.Silence.MinSpeech)
	info.DetectedSpeechIntervalCount = len(intervals)
	info.DetectedEffectiveDuration = sumIntervalDurations(intervals)
	info.EffectiveDuration = info.DetectedEffectiveDuration
	if len(intervals) == 0 {
		info.OutputPath = wavPath
		info.OutputDuration = duration
		info.DetectedEffectiveDuration = duration
		info.EffectiveDuration = duration
		return wavPath, info, nil
	}
	if outWAVPath == "" {
		ext := filepath.Ext(wavPath)
		outWAVPath = strings.TrimSuffix(wavPath, ext) + "_nosilence.wav"
	}
	if err := p.backend.RenderIntervalsToWAV(ctx, wavPath, outWAVPath, intervals, p.cfg.Segments.SampleRate); err != nil {
		return wavPath, info, err
	}
	info.OutputPath = outWAVPath
	if outputDuration, err := p.backend.ProbeDuration(ctx, outWAVPath, ProbeWAVFirst); err == nil {
		info.OutputDuration = outputDuration
	} else {
		info.OutputDuration = info.EffectiveDuration
	}
	return outWAVPath, info, nil
}

func (p *Processor) SplitWAVBySilenceGroups(ctx context.Context, wavPath string) ([]Segment, ProcessingInfo, error) {
	info := ProcessingInfo{InputPath: wavPath}
	duration, err := p.backend.ProbeDuration(ctx, wavPath, ProbeWAVFirst)
	if err != nil {
		return nil, info, err
	}
	info.InputDuration = duration
	silences, err := p.detectSilences(ctx, wavPath)
	if err != nil {
		return nil, info, err
	}
	info.DetectedSilenceCount = len(silences)
	intervals := InvertAndExpand(silences, duration, p.cfg.Silence.Padding, p.cfg.Silence.MinSpeech)
	info.DetectedSpeechIntervalCount = len(intervals)
	info.DetectedEffectiveDuration = sumIntervalDurations(intervals)
	if len(intervals) == 0 {
		info.DetectedEffectiveDuration = duration
		info.EffectiveDuration = duration
		return nil, info, nil
	}
	groups := GroupIntervalsByMaxSpan(intervals, p.cfg.Segments.MaxLength)
	info.SegmentGroupCount = len(groups)
	if len(groups) == 0 {
		return nil, info, nil
	}
	outDir := p.cfg.Segments.OutDir
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(wavPath), "out_segments")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, info, err
	}
	base := strings.TrimSuffix(filepath.Base(wavPath), filepath.Ext(wavPath))
	segments := make([]Segment, 0, len(groups))
	preserveInternalSilence := boolValue(p.cfg.Segments.PreserveInternalSilence, true)
	for idx, group := range groups {
		index := idx + 1
		segWAV := filepath.Join(outDir, fmt.Sprintf("%s_seg%03d.wav", base, index))
		if preserveInternalSilence {
			if err := p.backend.ExportWAV(ctx, wavPath, segWAV, group[0].Start, group[len(group)-1].End, p.cfg.Segments.SampleRate); err != nil {
				info.SegmentSkipped++
				continue
			}
		} else {
			if err := p.backend.RenderIntervalsToWAV(ctx, wavPath, segWAV, group, p.cfg.Segments.SampleRate); err != nil {
				info.SegmentSkipped++
				continue
			}
		}
		outOGG := filepath.Join(outDir, fmt.Sprintf("%s_part%03d.ogg", base, index))
		file := outOGG
		if err := p.backend.EncodeOpus(ctx, segWAV, outOGG, p.cfg.Segments.SampleRate, p.cfg.Segments.OpusBitrate); err != nil {
			file = segWAV
		}
		start := group[0].Start
		end := group[len(group)-1].End
		tempWAV := ""
		if boolValue(p.cfg.Segments.KeepTempWAV, true) {
			tempWAV = segWAV
		}
		segments = append(segments, Segment{
			Index:      index,
			File:       file,
			TempWAV:    tempWAV,
			Start:      start,
			End:        end,
			Cut:        end,
			Duration:   end - start,
			Intervals:  append([]Interval(nil), group...),
			SourceWAV:  wavPath,
			SourcePath: wavPath,
		})
		info.OutputFiles = append(info.OutputFiles, file)
		info.EffectiveDuration += sumIntervalDurations(group)
		if preserveInternalSilence {
			info.OutputDuration += end - start
		} else {
			info.OutputDuration += sumIntervalDurations(group)
		}
	}
	info.SegmentCount = len(segments)
	info.OutputPath = outDir
	return segments, info, nil
}

func boolValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func (p *Processor) RemoveSilenceByFixedSlicesAndMerge(ctx context.Context, wavPath, outMergedWAV string) (string, ProcessingInfo, error) {
	cfg := p.cfg
	info := ProcessingInfo{InputPath: wavPath}
	duration, err := p.backend.ProbeDuration(ctx, wavPath, ProbeWAVFirst)
	if err != nil {
		return wavPath, info, err
	}
	info.InputDuration = duration
	if outMergedWAV == "" {
		ext := filepath.Ext(wavPath)
		outMergedWAV = strings.TrimSuffix(wavPath, ext) + "_sliced_merged.wav"
	}
	tempDir := cfg.FixedTrim.TempDir
	if tempDir == "" {
		var err error
		tempDir, err = os.MkdirTemp(filepath.Dir(outMergedWAV), ".smartaudio-fixed-*")
		if err != nil {
			return wavPath, info, err
		}
		defer os.RemoveAll(tempDir)
	} else if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return wavPath, info, err
	}
	sliceDir := filepath.Join(tempDir, "slices")
	trimmedDir := filepath.Join(tempDir, "trimmed_slices")
	if err := os.MkdirAll(sliceDir, 0o755); err != nil {
		return wavPath, info, err
	}
	if err := os.MkdirAll(trimmedDir, 0o755); err != nil {
		return wavPath, info, err
	}
	base := strings.TrimSuffix(filepath.Base(wavPath), filepath.Ext(wavPath))
	slicePaths, err := p.backend.SplitWAVFixed(ctx, wavPath, sliceDir, base+"_slice", cfg.FixedTrim.SliceLength, cfg.Segments.SampleRate)
	if err != nil || len(slicePaths) == 0 {
		return wavPath, info, err
	}
	info.FixedSliceCount = len(slicePaths)
	type sliceResult struct {
		Index int
		Path  string
		Info  ProcessingInfo
		OK    bool
	}
	process := func(ctx context.Context, i int, sliceWAV string) sliceResult {
		trimmedWAV := filepath.Join(trimmedDir, fmt.Sprintf("%s_slice_trim%04d.wav", base, i+1))
		trimmed, trimInfo, err := p.withConfig(cfg).TrimLongSilencesFromWAV(ctx, sliceWAV, trimmedWAV)
		if err != nil {
			return sliceResult{Index: i + 1}
		}
		if trimmed == "" {
			return sliceResult{Index: i + 1}
		}
		return sliceResult{Index: i + 1, Path: trimmed, Info: trimInfo, OK: true}
	}
	results := processSlicesSettled(ctx, slicePaths, cfg.FixedTrim.Workers, process)
	paths := make([]string, 0, len(results))
	for _, r := range results {
		if !r.OK || r.Path == "" {
			info.FixedSliceSkipped++
			continue
		}
		info.DetectedSilenceCount += r.Info.DetectedSilenceCount
		info.DetectedSpeechIntervalCount += r.Info.DetectedSpeechIntervalCount
		info.DetectedEffectiveDuration += r.Info.DetectedEffectiveDuration
		info.EffectiveDuration += r.Info.EffectiveDuration
		d, err := p.backend.ProbeDuration(ctx, r.Path, ProbeWAVFirst)
		if err == nil && d >= cfg.FixedTrim.MinSegmentLength {
			paths = append(paths, r.Path)
			info.FixedSliceSucceeded++
			continue
		}
		info.FixedSliceSkipped++
	}
	if len(paths) == 0 {
		info.OutputPath = wavPath
		info.OutputDuration = duration
		info.EffectiveDuration = duration
		return wavPath, info, nil
	}
	if err := p.backend.ConcatWAV(ctx, paths, outMergedWAV); err != nil {
		return wavPath, info, err
	}
	info.OutputPath = outMergedWAV
	info.OutputFiles = append([]string(nil), paths...)
	if outputDuration, err := p.backend.ProbeDuration(ctx, outMergedWAV, ProbeWAVFirst); err == nil {
		info.OutputDuration = outputDuration
		info.EffectiveDuration = outputDuration
	} else if info.EffectiveDuration == 0 {
		info.EffectiveDuration = sumProbeDurations(ctx, p.backend, paths)
		info.OutputDuration = info.EffectiveDuration
	}
	return outMergedWAV, info, nil
}

func (p *Processor) withConfig(cfg Config) *Processor {
	return &Processor{cfg: cfg, backend: p.backend}
}

func (p *Processor) detectSilences(ctx context.Context, wavPath string) ([]Interval, error) {
	stats, err := p.backend.VolumeDetect(ctx, wavPath)
	if err != nil {
		return nil, err
	}
	for _, threshold := range BuildThresholds(stats, p.cfg.Silence) {
		minSilence := p.cfg.Silence.MinSilence
		if minSilence < time.Millisecond {
			minSilence = time.Millisecond
		}
		intervals, err := p.backend.SilenceDetect(ctx, wavPath, threshold, minSilence)
		if err != nil {
			return nil, err
		}
		if len(intervals) > 0 {
			return intervals, nil
		}
	}
	return nil, nil
}

func sumIntervalDurations(intervals []Interval) time.Duration {
	var total time.Duration
	for _, interval := range intervals {
		total += interval.Duration()
	}
	return total
}

func sumProbeDurations(ctx context.Context, backend Backend, paths []string) time.Duration {
	var total time.Duration
	for _, path := range paths {
		duration, err := backend.ProbeDuration(ctx, path, ProbeWAVFirst)
		if err == nil {
			total += duration
		}
	}
	return total
}

func processSlicesSettled[R any](ctx context.Context, items []string, workers int, fn func(context.Context, int, string) R) []R {
	if workers <= 0 {
		workers = 1
	}
	if workers > len(items) && len(items) > 0 {
		workers = len(items)
	}
	type job struct {
		index int
		path  string
	}
	type result struct {
		index int
		value R
	}
	jobs := make(chan job)
	results := make(chan result, len(items))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- result{index: j.index, value: fn(ctx, j.index, j.path)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, item := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{index: i, path: item}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	out := make([]R, len(items))
	for r := range results {
		out[r.index] = r.value
	}
	return out
}
