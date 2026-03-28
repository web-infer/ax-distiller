package syncx

import (
	"context"
	"sync"
)

type Work interface {
	Exec()
}

type WorkerPool[T Work] struct {
	queue   chan T
	wg      sync.WaitGroup
	workers int
}

func NewWorkerPool[T Work](workers int) *WorkerPool[T] {
	return &WorkerPool[T]{
		queue:   make(chan T),
		workers: workers,
	}
}

func (b *WorkerPool[T]) Add(work T) {
	b.wg.Add(1)
	go func() {
		b.queue <- work
	}()
}

func (b *WorkerPool[T]) doWork(work T) {
	defer b.wg.Done()
	work.Exec()
}

func (b *WorkerPool[T]) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-b.queue:
			b.doWork(j)
		}
	}
}

func (b *WorkerPool[T]) Start(ctx context.Context) {
	for range b.workers {
		go b.worker(ctx)
	}
}

func (b *WorkerPool[T]) Wait() {
	b.wg.Wait()
}
