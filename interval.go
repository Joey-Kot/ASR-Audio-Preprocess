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
	"math"
	"sort"
	"time"
)

type Interval struct {
	Start time.Duration
	End   time.Duration
}

func (i Interval) Duration() time.Duration {
	if i.End <= i.Start {
		return 0
	}
	return i.End - i.Start
}

func InvertAndExpand(silences []Interval, duration, padding, minSpeech time.Duration) []Interval {
	if duration <= 0 {
		return nil
	}
	sorted := append([]Interval(nil), silences...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})
	nonSilent := make([]Interval, 0, len(sorted)+1)
	prev := time.Duration(0)
	for _, silence := range sorted {
		if silence.Start > prev {
			nonSilent = append(nonSilent, Interval{Start: prev, End: silence.Start})
		}
		if silence.End > prev {
			prev = silence.End
		}
	}
	if prev < duration {
		nonSilent = append(nonSilent, Interval{Start: prev, End: duration})
	}
	if minSpeech <= 0 {
		minSpeech = 20 * time.Millisecond
	}
	expanded := make([]Interval, 0, len(nonSilent))
	for _, iv := range nonSilent {
		start := iv.Start - padding
		if start < 0 {
			start = 0
		}
		end := iv.End + padding
		if end > duration {
			end = duration
		}
		if len(expanded) == 0 || start > expanded[len(expanded)-1].End {
			expanded = append(expanded, Interval{Start: start, End: end})
		} else if end > expanded[len(expanded)-1].End {
			expanded[len(expanded)-1].End = end
		}
	}
	filtered := expanded[:0]
	for _, iv := range expanded {
		if iv.Duration() >= minSpeech {
			filtered = append(filtered, iv)
		}
	}
	return append([]Interval(nil), filtered...)
}

func GroupIntervalsByMaxSpan(intervals []Interval, maxSegmentLen time.Duration) [][]Interval {
	if maxSegmentLen <= 0 {
		maxSegmentLen = DefaultMaxSegmentLength
	}
	var groups [][]Interval
	var cur []Interval
	var segmentStart time.Duration
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
			segmentStart = 0
		}
	}
	eps := time.Microsecond
	for _, iv := range intervals {
		if iv.End <= iv.Start {
			continue
		}
		if iv.Duration() > maxSegmentLen+eps {
			flush()
			for start := iv.Start; start < iv.End-eps; start += maxSegmentLen {
				end := start + maxSegmentLen
				if end > iv.End {
					end = iv.End
				}
				groups = append(groups, []Interval{{Start: start, End: end}})
			}
			continue
		}
		if len(cur) == 0 {
			cur = append(cur, iv)
			segmentStart = iv.Start
			continue
		}
		if iv.End-segmentStart <= maxSegmentLen+eps {
			cur = append(cur, iv)
		} else {
			flush()
			cur = append(cur, iv)
			segmentStart = iv.Start
		}
	}
	flush()
	return groups
}

func BuildThresholds(stats VolumeStats, cfg SilenceConfig) []float64 {
	if cfg.ThresholdDB != nil {
		return []float64{*cfg.ThresholdDB}
	}
	if len(cfg.Thresholds) > 0 {
		out := make([]float64, 0, len(cfg.Thresholds))
		seen := map[float64]struct{}{}
		for _, v := range cfg.Thresholds {
			tv := clamp(v, thresholdFloor(cfg), thresholdCeil(cfg))
			if _, ok := seen[tv]; ok {
				continue
			}
			seen[tv] = struct{}{}
			out = append(out, tv)
		}
		return out
	}
	var raw []float64
	hasMax := stats.HasMax || (stats.Valid && !stats.HasMean)
	hasMean := stats.HasMean || (stats.Valid && !stats.HasMax)
	if hasMax && !math.IsInf(stats.MaxDB, 0) && !math.IsNaN(stats.MaxDB) {
		for _, offset := range []float64{18, 16, 14, 12, 10} {
			raw = append(raw, stats.MaxDB-offset)
		}
	} else if hasMean && !math.IsInf(stats.MeanDB, 0) && !math.IsNaN(stats.MeanDB) {
		base := stats.MeanDB - 8
		for _, offset := range []float64{0, 6, 12} {
			raw = append(raw, base+offset)
		}
	} else {
		raw = []float64{-35, -30, -25, -20}
	}
	out := make([]float64, 0, len(raw))
	seen := map[float64]struct{}{}
	for _, v := range raw {
		tv := clamp(v, thresholdFloor(cfg), thresholdCeil(cfg))
		if _, ok := seen[tv]; ok {
			continue
		}
		seen[tv] = struct{}{}
		out = append(out, tv)
	}
	return out
}

func thresholdFloor(cfg SilenceConfig) float64 {
	if cfg.ThresholdFloor == 0 {
		return -60
	}
	return cfg.ThresholdFloor
}

func thresholdCeil(cfg SilenceConfig) float64 {
	if cfg.ThresholdCeil == 0 {
		return -10
	}
	return cfg.ThresholdCeil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
