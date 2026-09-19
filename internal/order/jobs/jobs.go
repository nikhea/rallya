// Package jobs defines the order sweeper River job: expired PENDING holds
// are released on a schedule so inventory truth never depends on reads.
package jobs

import (
	"context"

	"github.com/riverqueue/river"

	"github.com/nikhea/rallya/internal/order/service"
)

// SweepExpiredOrdersArgs triggers hold-expiry sweeps. Empty args: the
// worker always sweeps a bounded batch.
type SweepExpiredOrdersArgs struct{}

func (SweepExpiredOrdersArgs) Kind() string { return "sweep_expired_orders" }

// SweepExpiredOrdersWorker releases expired holds in bounded batches.
type SweepExpiredOrdersWorker struct {
	river.WorkerDefaults[SweepExpiredOrdersArgs]
	Svc *service.OrderService
}

func (w *SweepExpiredOrdersWorker) Work(ctx context.Context, job *river.Job[SweepExpiredOrdersArgs]) error {
	if w.Svc == nil {
		return nil
	}
	_, err := w.Svc.SweepExpired(ctx, 100)
	return err
}
