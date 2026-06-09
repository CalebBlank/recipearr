// Package scheduler runs due sources on a fixed tick. It is DB-driven (each tick selects
// sources whose last_checked_at + interval has elapsed), so it is restart-safe and reflects
// interval edits made in the UI without any register/unregister churn.
package scheduler

import (
	"context"
	"time"

	"recipearr/internal/pipeline"
	"recipearr/internal/store"
)

type Scheduler struct {
	st   *store.Store
	svc  *pipeline.Service
	tick time.Duration
	sem  chan struct{} // bounds concurrent source runs
}

func New(st *store.Store, svc *pipeline.Service) *Scheduler {
	return &Scheduler{
		st:   st,
		svc:  svc,
		tick: time.Minute,
		sem:  make(chan struct{}, 3),
	}
}

// Run blocks until ctx is cancelled, processing due sources every tick.
func (s *Scheduler) Run(ctx context.Context) {
	s.runDue(ctx) // catch anything already due at startup
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runDue(ctx)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context) {
	sources, err := s.st.ListEnabledSources()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, src := range sources {
		if !due(src, now) {
			continue
		}
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		go func(src *store.Source) {
			defer func() { <-s.sem }()
			cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			// Poll branches on seeded state; per-source locking (in the pipeline) makes an
			// overlapping manual run safe.
			_, _ = s.svc.Poll(cctx, src)
		}(src)
	}
}

func due(src *store.Source, now time.Time) bool {
	if src.LastCheckedAt == nil {
		return true
	}
	interval := time.Duration(src.PollIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	return now.Sub(*src.LastCheckedAt) >= interval
}
