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
		sampleRate = p.cfg.Segments.OutputSampleRate
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
		if len(silences) == 0 {
			info.OutputPath = wavPath
			info.OutputDuration = duration
			info.DetectedEffectiveDuration = duration
			info.EffectiveDuration = duration
			return wavPath, info, nil
		}
		return "", info, nil
	}
	if outWAVPath == "" {
		outWAVPath = filepath.Join(filepath.Dir(wavPath), newWorkFileStem()+"_nosilence.wav")
	}
	if err := p.backend.RenderIntervalsToWAV(ctx, wavPath, outWAVPath, intervals, p.cfg.Segments.OutputSampleRate); err != nil {
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
		if len(silences) == 0 {
			info.DetectedEffectiveDuration = duration
			info.EffectiveDuration = duration
		}
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
	base := newWorkFileStem()
	preserveInternalSilence := boolValue(p.cfg.Segments.PreserveInternalSilence, true)
	output := p.cfg.Segments.audioOutput()
	type segmentResult struct {
		Segment        Segment
		OutputDuration time.Duration
		Effective      time.Duration
		Done           bool
		Skipped        bool
		Err            error
	}
	results, segmentErr := MapSettledOrderedUntilError(ctx, groups, p.cfg.Segments.Workers, func(ctx context.Context, idx int, group []Interval) (segmentResult, error) {
		index := idx + 1
		segWAV := filepath.Join(outDir, fmt.Sprintf("%s_seg%03d.wav", base, index))
		if preserveInternalSilence {
			if err := p.backend.ExportWAV(ctx, wavPath, segWAV, group[0].Start, group[len(group)-1].End, p.cfg.Segments.OutputSampleRate); err != nil {
				return segmentResult{Done: true, Skipped: true}, nil
			}
		} else {
			if err := p.backend.RenderIntervalsToWAV(ctx, wavPath, segWAV, group, p.cfg.Segments.OutputSampleRate); err != nil {
				return segmentResult{Done: true, Skipped: true}, nil
			}
		}
		outAudio := filepath.Join(outDir, fmt.Sprintf("%s_part%03d.%s", base, index, output.Extension))
		file := outAudio
		if err := p.backend.EncodeAudio(ctx, segWAV, outAudio, p.cfg.Segments.OutputSampleRate, output.Format, output.Codec, output.Bitrate, output.SampleFormat); err != nil {
			if !p.cfg.Segments.allowEncodeFallbackToWAV(output) {
				err := fmt.Errorf("encode segment %d as %s/%s: %w", index, output.Format, output.Codec, err)
				return segmentResult{Done: true, Err: err}, err
			}
			file = segWAV
		}
		start := group[0].Start
		end := group[len(group)-1].End
		tempWAV := ""
		if boolValue(p.cfg.Segments.KeepTempWAV, true) {
			tempWAV = segWAV
		}
		segment := Segment{
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
		}
		result := segmentResult{
			Segment:   segment,
			Effective: sumIntervalDurations(group),
			Done:      true,
		}
		if preserveInternalSilence {
			result.OutputDuration = end - start
		} else {
			result.OutputDuration = result.Effective
		}
		return result, nil
	})
	segments := make([]Segment, 0, len(groups))
	for _, r := range results {
		if !r.Done {
			if err := ctx.Err(); err != nil {
				return segments, info, err
			}
			info.SegmentSkipped++
			continue
		}
		if r.Err != nil {
			return segments, info, r.Err
		}
		if r.Skipped {
			info.SegmentSkipped++
			continue
		}
		segments = append(segments, r.Segment)
		info.OutputFiles = append(info.OutputFiles, r.Segment.File)
		info.EffectiveDuration += r.Effective
		info.OutputDuration += r.OutputDuration
	}
	if segmentErr != nil {
		return segments, info, segmentErr
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
		outMergedWAV = filepath.Join(filepath.Dir(wavPath), newWorkFileStem()+"_sliced_merged.wav")
	}
	tempRoot := cfg.FixedTrim.TempDir
	cleanupTempRoot := false
	if tempRoot == "" {
		var err error
		tempRoot, err = os.MkdirTemp(filepath.Dir(outMergedWAV), ".smartaudio-fixed-*")
		if err != nil {
			return wavPath, info, err
		}
		cleanupTempRoot = true
	} else if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return wavPath, info, err
	}
	if cleanupTempRoot {
		defer os.RemoveAll(tempRoot)
	}
	runDir, err := os.MkdirTemp(tempRoot, "run-*")
	if err != nil {
		return wavPath, info, err
	}
	if cleanupTempRoot {
		defer os.RemoveAll(runDir)
	}
	sliceDir := filepath.Join(runDir, "slices")
	trimmedDir := filepath.Join(runDir, "trimmed_slices")
	if err := os.MkdirAll(sliceDir, 0o755); err != nil {
		return wavPath, info, err
	}
	if err := os.MkdirAll(trimmedDir, 0o755); err != nil {
		return wavPath, info, err
	}
	base := newWorkFileStem()
	slicePaths, err := p.backend.SplitWAVFixed(ctx, wavPath, sliceDir, base+"_slice", cfg.FixedTrim.SliceLength, cfg.Segments.OutputSampleRate)
	if err != nil {
		return wavPath, info, err
	}
	if len(slicePaths) == 0 {
		return "", info, nil
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
		if trimmed == "" || trimInfo.OutputDuration == 0 {
			return sliceResult{Index: i + 1, Info: trimInfo}
		}
		return sliceResult{Index: i + 1, Path: trimmed, Info: trimInfo, OK: true}
	}
	results := MapSettledOrdered(ctx, slicePaths, cfg.FixedTrim.Workers, process)
	type validatedSlice struct {
		Result sliceResult
		Valid  bool
	}
	validated := MapSettledOrdered(ctx, results, cfg.FixedTrim.Workers, func(ctx context.Context, _ int, r sliceResult) validatedSlice {
		if !r.OK || r.Path == "" {
			return validatedSlice{Result: r}
		}
		d, err := p.backend.ProbeDuration(ctx, r.Path, ProbeWAVFirst)
		if err == nil && d >= cfg.FixedTrim.MinSegmentLength {
			return validatedSlice{Result: r, Valid: true}
		}
		return validatedSlice{Result: r}
	})
	paths := make([]string, 0, len(validated))
	for _, v := range validated {
		r := v.Result
		info.DetectedSilenceCount += r.Info.DetectedSilenceCount
		info.DetectedSpeechIntervalCount += r.Info.DetectedSpeechIntervalCount
		info.DetectedEffectiveDuration += r.Info.DetectedEffectiveDuration
		info.EffectiveDuration += r.Info.EffectiveDuration
		if v.Valid {
			paths = append(paths, r.Path)
			info.FixedSliceSucceeded++
			continue
		}
		info.FixedSliceSkipped++
	}
	if len(paths) == 0 {
		return "", info, nil
	}
	if err := p.backend.ConcatWAV(ctx, paths, outMergedWAV); err != nil {
		return wavPath, info, err
	}
	info.OutputPath = outMergedWAV
	info.OutputFiles = []string{outMergedWAV}
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
	durations := MapSettledOrdered(ctx, paths, 0, func(ctx context.Context, _ int, path string) time.Duration {
		duration, err := backend.ProbeDuration(ctx, path, ProbeWAVFirst)
		if err == nil {
			return duration
		}
		return 0
	})
	for _, duration := range durations {
		total += duration
	}
	return total
}
