package client

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type stats struct {
	active atomic.Int64
	total  atomic.Int64
}

func (s *stats) connStart() {
	s.active.Add(1)
	s.total.Add(1)
}

func (s *stats) connDone() {
	s.active.Add(-1)
}

func (s *stats) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var prevTotal int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			total := s.total.Load()
			if total == prevTotal {
				continue
			}
			prevTotal = total
			slog.Info("stats",
				"total", total,
				"active", s.active.Load(),
			)
		}
	}
}
