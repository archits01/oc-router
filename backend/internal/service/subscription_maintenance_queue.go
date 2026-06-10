package service

import (
	"fmt"
	"log"
	"sync"
)

// SubscriptionMaintenanceQueue "+ "
//
type SubscriptionMaintenanceQueue struct {
	queue  chan func()
	wg     sync.WaitGroup
	stop   sync.Once
	mu     sync.RWMutex // protects atomicity of closed flag and channel operations
	closed bool
}

func NewSubscriptionMaintenanceQueue(workerCount, queueSize int) *SubscriptionMaintenanceQueue {
	if workerCount <= 0 {
		workerCount = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}

	q := &SubscriptionMaintenanceQueue{
		queue: make(chan func(), queueSize),
	}

	q.wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer q.wg.Done()
			for fn := range q.queue {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("SubscriptionMaintenance worker panic: %v", r)
						}
					}()
					fn()
				}()
			}
		}(i)
	}

	return q
}

// TryEnqueue
//
//
func (q *SubscriptionMaintenanceQueue) TryEnqueue(task func()) error {
	if q == nil {
		return fmt.Errorf("maintenance queue is nil")
	}
	if task == nil {
		return fmt.Errorf("maintenance task is nil")
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return fmt.Errorf("maintenance queue stopped")
	}

	select {
	case q.queue <- task:
		return nil
	default:
		return fmt.Errorf("maintenance queue full")
	}
}

func (q *SubscriptionMaintenanceQueue) Stop() {
	if q == nil {
		return
	}
	q.stop.Do(func() {
		q.mu.Lock()
		q.closed = true
		close(q.queue)
		q.mu.Unlock()
		q.wg.Wait()
	})
}
