package profile

import (
	log "github.com/rs/zerolog"
)

type ProfileOption func(profile *Profiler)

func WithPID(pid int) ProfileOption {
	return func(t *Profiler) {
		t.pid = pid
	}
}

func WithSamplingPeriodMillis(period uint64) ProfileOption {
	return func(t *Profiler) {
		t.samplingPeriodMillis = period
	}
}

func WithProbe(probe []byte) ProfileOption {
	return func(t *Profiler) {
		t.probe = probe
	}
}

func WithProbeName(name string) ProfileOption {
	return func(t *Profiler) {
		t.probeName = name
	}
}

func WithMapStackTraces(name string) ProfileOption {
	return func(t *Profiler) {
		t.mapStackTraces = name
	}
}

func WithMapHistogram(name string) ProfileOption {
	return func(t *Profiler) {
		t.mapHistogram = name
	}
}

func WithLogger(logger log.Logger) ProfileOption {
	return func(t *Profiler) {
		t.logger = logger
	}
}
