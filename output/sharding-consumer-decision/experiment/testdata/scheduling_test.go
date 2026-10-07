package loki

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// Controlled blocking, rather than sleeps, makes the scheduling counterexamples
// deterministic. All goroutines and borrowed batches complete before each test ends.
func completed(ch <-chan error) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestConcurrentSameStream(t *testing.T) {
	for _, mode := range []string{"direct", "sharded", "caller_gate"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var mu sync.Mutex
				var order []int
				sink := consumerFunc(func(_ context.Context, b Batch) error {
					id := b.streams[0].ID
					if id == 1 {
						close(started)
						<-release
					}
					mu.Lock()
					order = append(order, id)
					mu.Unlock()
					return nil
				})
				var c Consumer = sink
				if mode == "sharded" {
					s := NewShardingConsumer(2, sink)
					defer s.Stop()
					c = s
				}
				if mode == "caller_gate" {
					c = newGate(2, sink)
				}
				first, second := make(chan error, 1), make(chan error, 1)
				go func() { first <- c.Consume(context.Background(), batch(0, 1)) }()
				<-started
				go func() { second <- c.Consume(context.Background(), batch(0, 2)) }()
				synctest.Wait()
				if got := completed(second); got != (mode == "direct") {
					t.Fatalf("second finished early: %v", got)
				}
				close(release)
				synctest.Wait()
				want := []int{1, 2}
				if mode == "direct" {
					want = []int{2, 1}
				}
				if !reflect.DeepEqual(order, want) {
					t.Fatalf("order %v, want %v", order, want)
				}
			})
		})
	}
}

func TestOneReaderProvidesBackpressureAndOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		var readCount atomic.Int32
		var delivered []int
		sink := consumerFunc(func(_ context.Context, b Batch) error {
			if b.streams[0].ID == 1 {
				close(started)
				<-release
			}
			delivered = append(delivered, b.streams[0].ID)
			return nil
		})
		go func() {
			for i := 1; i <= 2; i++ {
				readCount.Add(1)
				_ = sink.Consume(context.Background(), batch(0, i))
			}
		}()
		<-started
		synctest.Wait()
		if readCount.Load() != 1 {
			t.Fatal("reader advanced during blocked Consume")
		}
		close(release)
		synctest.Wait()
		if !reflect.DeepEqual(delivered, []int{1, 2}) {
			t.Fatal(delivered)
		}
	})
}

func TestUnrelatedStreamCollision(t *testing.T) {
	for _, mode := range []string{"direct", "sharded", "caller_gate"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				sink := consumerFunc(func(_ context.Context, b Batch) error {
					if b.streams[0].Labels == 0 {
						close(started)
						<-release
					}
					return nil
				})
				var c Consumer = sink
				if mode == "sharded" {
					s := NewShardingConsumer(2, sink)
					defer s.Stop()
					c = s
				}
				if mode == "caller_gate" {
					c = newGate(2, sink)
				}
				go func() { _ = c.Consume(context.Background(), batch(0, 1)) }()
				<-started
				second := make(chan error, 1)
				go func() { second <- c.Consume(context.Background(), batch(2, 2)) }()
				synctest.Wait()
				if got := completed(second); got != (mode == "direct") {
					t.Fatalf("second finished: %v", got)
				}
				close(release)
				synctest.Wait()
			})
		})
	}
}

func TestMultiStreamDispatchWaitsBehindBusyFirstShard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		var secondShardRan atomic.Bool
		s := NewShardingConsumer(2, consumerFunc(func(_ context.Context, b Batch) error {
			if b.streams[0].ID == 1 {
				close(started)
				<-release
			}
			if b.streams[0].Labels == 1 {
				secondShardRan.Store(true)
			}
			return nil
		}))
		defer s.Stop()
		go func() { _ = s.Consume(context.Background(), batch(0, 1)) }()
		<-started
		go func() { _ = s.Consume(context.Background(), Batch{[]Stream{{Labels: 0, ID: 2}, {Labels: 1, ID: 3}}}) }()
		synctest.Wait()
		if secondShardRan.Load() {
			t.Fatal("expected current dispatch loop to wait on busy shard")
		}
		close(release)
		synctest.Wait()
		if !secondShardRan.Load() {
			t.Fatal("second shard never ran")
		}
	})
}

func TestShardingParallelizesOneMultiStreamCall(t *testing.T) {
	for _, mode := range []string{"direct", "sharded"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var secondRan atomic.Bool
				sink := consumerFunc(func(_ context.Context, b Batch) error {
					for _, stream := range b.Streams() {
						if stream.ID == 1 {
							close(started)
							<-release
						} else {
							secondRan.Store(true)
						}
					}
					return nil
				})
				var c Consumer = sink
				if mode == "sharded" {
					s := NewShardingConsumer(2, sink)
					defer s.Stop()
					c = s
				}
				go func() { _ = c.Consume(context.Background(), Batch{[]Stream{{Labels: 0, ID: 1}, {Labels: 1, ID: 2}}}) }()
				<-started
				synctest.Wait()
				if got := secondRan.Load(); got != (mode == "sharded") {
					t.Fatalf("second stream ran: %v", got)
				}
				close(release)
				synctest.Wait()
			})
		})
	}
}

func TestAcceptedCallStillNeedsDownstreamCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		s := NewShardingConsumer(1, consumerFunc(func(_ context.Context, _ Batch) error { close(started); <-release; return nil }))
		defer s.Stop()
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- s.Consume(ctx, batch(0, 1)) }()
		<-started
		cancel()
		synctest.Wait()
		if completed(result) {
			t.Fatal("returned while downstream still using batch")
		}
		close(release)
		synctest.Wait()
		if !completed(result) {
			t.Fatal("did not finish")
		}
	})
}
