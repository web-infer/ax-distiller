package syncx

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
)

// BundledRequests is an object that provides a means to wait until all the
// given asynchronous operations resolve and all the child BundledRequests have
// all finished.
type BundledRequests[ID comparable] struct {
	subscribed int
	childMutex sync.Mutex
	children   []chan bool
	reqsdone   atomic.Bool // this exists for asserting
	fired      atomic.Bool

	logger  *slog.Logger
	ctx     context.Context
	targets []ID
	req     chan struct{}
	done    chan bool
}

// NewBundledRequests creates a BundledRequests object.
//
// This should not be reused, the goroutine it spawns on creation will handle
// the lifecycle of the objects such that they will be destroyed when the
// request completes or is canceled
func NewBundledRequests[ID comparable](ctx context.Context, logger *slog.Logger, targets []ID) *BundledRequests[ID] {
	out := &BundledRequests[ID]{
		logger:  logger.WithGroup("bundled_requests"),
		targets: targets,
		req:     make(chan struct{}),
		done:    make(chan bool),
		ctx:     ctx,
	}
	go out.waitRoutine()
	return out
}

func (s *BundledRequests[ID]) waitRoutine() {
	// wait for all subscriptions to resolve (we wait this first, since
	// subscriptions can create child requests)
waitsub:
	for {
		select {
		case <-s.ctx.Done():
			s.fired.Store(true) // must be set before sending to done
			s.done <- false
			return
		case <-s.req:
			s.subscribed++
			s.logger.Debug("request update", "subscribed", s.subscribed, "total", len(s.targets))
			if s.subscribed > len(s.targets) {
				panic("subscribed count exceeds target count! this should never happen")
			}
			if s.subscribed == len(s.targets) {
				break waitsub
			}
		}
	}

	// assert in AddChild ensures that no modification is done to s.children
	// after this line
	s.reqsdone.Store(true)

	s.logger.Debug("start child wait", "total", len(s.children))
	for i, childRequest := range s.children {
		select {
		case <-s.ctx.Done():
			s.fired.Store(true) // must be set before sending to done
			s.done <- false
			return
		case <-childRequest:
			// if child's done closed -> child context canceled -> ignore it /
			// treat it as a done
		}
		s.logger.Debug("pass child wait", "progress", i+1, "total", len(s.children))
	}

	s.logger.Debug("send done")
	s.fired.Store(true) // must be set before sending to done
	s.done <- true
}

// AddChild adds a child BundledRequests, it will panic if all requests have
// already been resolved.
func (s *BundledRequests[ID]) AddChild(child *BundledRequests[ID]) {
	if s.reqsdone.Load() {
		panic("attempted to add child after all targets have resolved")
	}
	if s.fired.Load() {
		panic("cannot resolve after fired")
	}
	// we lock because multiple goroutines may use AddChild at the same time
	defer s.childMutex.Unlock()
	s.childMutex.Lock()
	s.children = append(s.children, child.done)
}

// Resolve indicates that a request has finished based on its ID. This method
// is goroutine-safe.
func (s *BundledRequests[ID]) Resolve(id ID) {
	// no need to lock this as `targets` will not be modified
	if !slices.Contains(s.targets, id) {
		panic(fmt.Errorf("resolved id is not part of BundledRequests[ID]: %v", id))
	}
	if s.fired.Load() {
		panic("cannot resolve after fired")
	}
	s.req <- struct{}{}
}

// Done returns a channel that will fire true upon done (all requests and
// child BundledRequests have finished). If the request context is canceled
// before it is done, it will fire false.
func (s *BundledRequests[ID]) Done() <-chan bool {
	return s.done
}

// Cleanup cleans up empty channels after the request has fired done, this
// should always be called after done fires.
func (s *BundledRequests[ID]) Cleanup() {
	if !s.fired.Load() {
		panic("should not Cleanup() before fired! prefer cancelling context, then calling Cleanup()")
	}
	close(s.req)
	close(s.done)
}
