package loki

import (
	"context"
	"errors"
	"github.com/prometheus/common/model"
)

var ErrConsumerStopped = errors.New("consumer stopped")

type Stream struct {
	Labels model.LabelSet
	ID     int
}
type Batch struct{ streams []Stream }

func NewBatch() Batch                { return Batch{} }
func (b *Batch) Add(s Stream)        { b.streams = append(b.streams, s) }
func (b Batch) Streams() []Stream    { return b.streams }
func (b Batch) StreamLen() int       { return len(b.streams) }
func batch(key uint64, id int) Batch { return Batch{[]Stream{{model.LabelSet(key), id}}} }

type Consumer interface {
	Consume(context.Context, Batch) error
}
type consumerFunc func(context.Context, Batch) error

func (f consumerFunc) Consume(ctx context.Context, b Batch) error { return f(ctx, b) }

// gate is a small example of serialization on the caller's goroutine. It is
// deliberately only single-stream, and is not a proposed production API.
type gate struct {
	slots []chan struct{}
	next  Consumer
}

func newGate(n int, next Consumer) *gate {
	g := &gate{slots: make([]chan struct{}, n), next: next}
	for i := range g.slots {
		g.slots[i] = make(chan struct{}, 1)
	}
	return g
}
func (g *gate) Consume(ctx context.Context, b Batch) error {
	slot := g.slots[uint64(b.streams[0].Labels)%uint64(len(g.slots))]
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
		return g.next.Consume(ctx, b)
	case <-ctx.Done():
		return ctx.Err()
	}
}
