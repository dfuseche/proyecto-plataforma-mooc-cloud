// Package queuemetrics lee el estado de la cola Asynq y lo publica en Cloud
// Monitoring como métricas personalizadas. La métrica "backlog" (tareas
// pendientes + activas) es la señal de autoscaling del grupo de workers: ver
// docs/entrega3/guia-escenario3-autoscaling.md.
package queuemetrics

import (
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// QueueSource es la parte de asynq.Inspector que usa Collect. Se declara como
// interfaz para poder probar la suma de colas sin Redis.
type QueueSource interface {
	Queues() ([]string, error)
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

// Snapshot es el estado sumado de las colas que consumen los workers.
type Snapshot struct {
	Pending        int
	Active         int
	Retry          int
	Archived       int
	ProcessedTotal int
	FailedTotal    int
	// OldestPending es la antigüedad de la tarea pendiente más vieja (0 si no hay pendientes).
	OldestPending time.Duration
}

// Backlog son los trabajos que necesitan un consumidor: los que esperan y los
// que se están procesando. Se cuentan los activos porque una cola sin
// pendientes puede tener trabajos en curso, y retirar workers en ese momento
// los interrumpiría.
func (s Snapshot) Backlog() int {
	return s.Pending + s.Active
}

// String es la línea que el exportador escribe en el log por cada muestra;
// en Cloud Logging queda como serie de respaldo de las métricas.
func (s Snapshot) String() string {
	return fmt.Sprintf("backlog=%d pending=%d active=%d retry=%d archived=%d oldest_pending_s=%.1f processed_total=%d failed_total=%d",
		s.Backlog(), s.Pending, s.Active, s.Retry, s.Archived, s.OldestPending.Seconds(), s.ProcessedTotal, s.FailedTotal)
}

// Collect suma el estado de las colas indicadas. Una cola en la que nunca se
// encoló nada no existe en Redis y cuenta como vacía (GetQueueInfo fallaría
// con ella, por eso primero se listan las existentes).
func Collect(src QueueSource, queues []string) (Snapshot, error) {
	existing, err := src.Queues()
	if err != nil {
		return Snapshot{}, fmt.Errorf("listing asynq queues: %w", err)
	}
	present := make(map[string]bool, len(existing))
	for _, q := range existing {
		present[q] = true
	}

	var s Snapshot
	for _, q := range queues {
		if !present[q] {
			continue
		}
		info, err := src.GetQueueInfo(q)
		if err != nil {
			return Snapshot{}, fmt.Errorf("reading asynq queue %q: %w", q, err)
		}
		s.Pending += info.Pending
		s.Active += info.Active
		s.Retry += info.Retry
		s.Archived += info.Archived
		s.ProcessedTotal += info.ProcessedTotal
		s.FailedTotal += info.FailedTotal
		if info.Latency > s.OldestPending {
			s.OldestPending = info.Latency
		}
	}
	return s, nil
}
