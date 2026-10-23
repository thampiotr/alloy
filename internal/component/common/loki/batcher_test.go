package loki

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestStreamBatcherV1(t *testing.T) {
	labels := model.LabelSet{"service_name": "app"}
	ts := time.Unix(0, 0)

	t.Run("flush sends added entry", func(t *testing.T) {
		recv := NewLogsReceiver(WithChannel(make(chan Entry, 1)))
		b := NewStreamBatcherV1(recv, labels)

		require.True(t, b.Add(push.Entry{Timestamp: ts, Line: "line 1"}))
		require.NoError(t, b.Flush(t.Context()))

		e := <-recv.Chan()
		require.Equal(t, labels, e.Labels)
		require.Equal(t, push.Entry{Timestamp: ts, Line: "line 1"}, e.Entry)
	})

	t.Run("flush without add sends nothing", func(t *testing.T) {
		recv := NewLogsReceiver(WithChannel(make(chan Entry, 1)))
		b := NewStreamBatcherV1(recv, labels)

		require.NoError(t, b.Flush(t.Context()))
		require.Len(t, recv.Chan(), 0)
	})

	t.Run("flush does not resend flushed entry", func(t *testing.T) {
		recv := NewLogsReceiver(WithChannel(make(chan Entry, 2)))
		b := NewStreamBatcherV1(recv, labels)

		require.True(t, b.Add(push.Entry{Timestamp: ts, Line: "line 1"}))
		require.NoError(t, b.Flush(t.Context()))
		require.NoError(t, b.Flush(t.Context()))
		require.Len(t, recv.Chan(), 1)
	})

	t.Run("failed flush drops entry", func(t *testing.T) {
		recv := NewLogsReceiver(WithChannel(make(chan Entry)))
		b := NewStreamBatcherV1(recv, labels)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.True(t, b.Add(push.Entry{Timestamp: ts, Line: "line 1"}))
		require.Error(t, b.Flush(ctx))
		require.NoError(t, b.Flush(ctx))
	})
}

func TestStreamBatcherV2(t *testing.T) {
	labels := model.LabelSet{"service_name": "app"}
	ts := time.Unix(0, 0)

	t.Run("add asks for flush at flush size", func(t *testing.T) {
		b := NewStreamBatcherV2(NewCollectingConsumer(), labels, 3)

		require.False(t, b.Add(push.Entry{Timestamp: ts, Line: "line 1"}))
		require.False(t, b.Add(push.Entry{Timestamp: ts, Line: "line 2"}))
		require.True(t, b.Add(push.Entry{Timestamp: ts, Line: "line 3"}))
	})

	t.Run("flush passes added entries as one stream", func(t *testing.T) {
		consumer := NewCollectingConsumer()
		b := NewStreamBatcherV2(consumer, labels, 10)

		b.Add(push.Entry{Timestamp: ts, Line: "line 1"})
		b.Add(push.Entry{Timestamp: ts, Line: "line 2"})
		require.NoError(t, b.Flush(t.Context()))

		batches := consumer.Batches()
		require.Len(t, batches, 1)
		streams := batches[0].Streams()
		require.Len(t, streams, 1)
		require.Equal(t, labels, streams[0].Labels)
		require.Equal(t, []push.Entry{
			{Timestamp: ts, Line: "line 1"},
			{Timestamp: ts, Line: "line 2"},
		}, streams[0].Entries)
	})

	t.Run("flush without add does not call consumer", func(t *testing.T) {
		consumer := NewCollectingConsumer()
		b := NewStreamBatcherV2(consumer, labels, 10)

		require.NoError(t, b.Flush(t.Context()))
		require.Len(t, consumer.Batches(), 0)
	})

	t.Run("flush after flush only passes new entries", func(t *testing.T) {
		consumer := NewCollectingConsumer()
		b := NewStreamBatcherV2(consumer, labels, 10)

		b.Add(push.Entry{Timestamp: ts, Line: "line 1"})
		b.Add(push.Entry{Timestamp: ts, Line: "line 2"})
		require.NoError(t, b.Flush(t.Context()))

		b.Add(push.Entry{Timestamp: ts, Line: "line 3"})
		require.NoError(t, b.Flush(t.Context()))

		batches := consumer.Batches()
		require.Len(t, batches, 2)
		require.Equal(t, []push.Entry{
			{Timestamp: ts, Line: "line 1"},
			{Timestamp: ts, Line: "line 2"},
		}, batches[0].Streams()[0].Entries)
		require.Equal(t, []push.Entry{
			{Timestamp: ts, Line: "line 3"},
		}, batches[1].Streams()[0].Entries)
	})

	t.Run("failed flush drops entries", func(t *testing.T) {
		var calls int
		consumer := consumerFunc{consume: func(_ context.Context, _ Batch) error {
			calls++
			return errors.New("consume failed")
		}}
		b := NewStreamBatcherV2(consumer, labels, 10)

		b.Add(push.Entry{Timestamp: ts, Line: "line 1"})
		require.Error(t, b.Flush(t.Context()))
		require.NoError(t, b.Flush(t.Context()))
		require.Equal(t, 1, calls)
	})
}

// TestStreamBatcherV2_Retry checks the POC contract: when a downstream component
// stops during a config reload, ErrConsumerStopped reaches the batcher, and the
// batcher resends the entries as they were added to the new downstream.
func TestStreamBatcherV2_Retry(t *testing.T) {
	labels := model.LabelSet{"service_name": "app"}
	original := []push.Entry{
		{Timestamp: time.Unix(1, 0), Line: "line 1"},
		{Timestamp: time.Unix(2, 0), Line: "line 2"},
	}

	// mutateAndStop changes every entry in place, as a downstream component is
	// allowed to, and then reports that it has stopped.
	mutateAndStop := func(batch Batch) error {
		for _, s := range batch.Streams() {
			for i := range s.Entries {
				s.Entries[i].Line = "mutated"
				s.Entries[i].Timestamp = time.Unix(99, 0)
				s.Entries[i].StructuredMetadata = push.LabelsAdapter{{Name: "mutated", Value: "true"}}
			}
		}
		return ErrConsumerStopped
	}

	addAll := func(b *StreamBatcherV2, entries []push.Entry) {
		for _, e := range entries {
			b.Add(e)
		}
	}

	t.Run("swapped downstream gets the original entries", func(t *testing.T) {
		var (
			newSink = NewCollectingConsumer()
			fanout  *FanoutV2
			calls   int
			created int64
		)
		oldSink := consumerFunc{consume: func(_ context.Context, batch Batch) error {
			calls++
			created = batch.Streams()[0].Created()
			// The reload switches the source to the new downstream while
			// this call is in flight.
			fanout.UpdateChildren([]LogsReceiver{NewLogsReceiverV2(newSink, "new")})
			return mutateAndStop(batch)
		}}
		fanout = NewFanoutV2([]LogsReceiver{NewLogsReceiverV2(oldSink, "old")})

		b := NewStreamBatcherV2(fanout, labels, 10)
		addAll(b, original)
		require.NoError(t, b.Flush(t.Context()))

		require.Equal(t, 1, calls)
		batches := newSink.Batches()
		require.Len(t, batches, 1)
		stream := batches[0].Streams()[0]
		require.Equal(t, labels, stream.Labels)
		require.Equal(t, original, stream.Entries)
		require.Equal(t, created, stream.Created(), "a retry keeps the created time of the first attempt")

		// The next flush only sends what was added after the retry.
		next := push.Entry{Timestamp: time.Unix(3, 0), Line: "line 3"}
		b.Add(next)
		require.NoError(t, b.Flush(t.Context()))
		batches = newSink.Batches()
		require.Len(t, batches, 2)
		require.Equal(t, []push.Entry{next}, batches[1].Streams()[0].Entries)
	})

	t.Run("stopped processor passes the error back to the source", func(t *testing.T) {
		// file -> process -> write becomes file -> relabel -> write2, and both
		// process and write are removed while a batch is in flight.
		var (
			newSink      = NewCollectingConsumer()
			sourceFanout *FanoutV2
		)
		oldSink := consumerFunc{consume: func(_ context.Context, batch Batch) error {
			sourceFanout.UpdateChildren([]LogsReceiver{NewLogsReceiverV2(newSink, "relabel")})
			return mutateAndStop(batch)
		}}
		processFanout := NewFanoutV2([]LogsReceiver{NewLogsReceiverV2(oldSink, "write")})
		process := consumerFunc{consume: func(ctx context.Context, batch Batch) error {
			// A processor only passes on what its downstream returned.
			return processFanout.Consume(ctx, batch)
		}}
		sourceFanout = NewFanoutV2([]LogsReceiver{NewLogsReceiverV2(process, "process")})

		b := NewStreamBatcherV2(sourceFanout, labels, 10)
		addAll(b, original)
		require.NoError(t, b.Flush(t.Context()))

		batches := newSink.Batches()
		require.Len(t, batches, 1)
		require.Equal(t, original, batches[0].Streams()[0].Entries)
	})

	t.Run("fanout children that already accepted get duplicates", func(t *testing.T) {
		// process -> write1, write2 becomes process -> write1, write3.
		var (
			write1 = NewCollectingConsumer()
			write3 = NewCollectingConsumer()
			fanout *FanoutV2
		)
		write2 := consumerFunc{consume: func(_ context.Context, batch Batch) error {
			fanout.UpdateChildren([]LogsReceiver{
				NewLogsReceiverV2(write1, "write1"),
				NewLogsReceiverV2(write3, "write3"),
			})
			return mutateAndStop(batch)
		}}
		fanout = NewFanoutV2([]LogsReceiver{
			NewLogsReceiverV2(write1, "write1"),
			NewLogsReceiverV2(write2, "write2"),
		})

		b := NewStreamBatcherV2(fanout, labels, 10)
		addAll(b, original)
		require.NoError(t, b.Flush(t.Context()))

		// Delivery is at-least-once: write1 gets the batch on the first attempt
		// and again on the retry.
		require.Len(t, write1.Batches(), 2)
		for _, batch := range write1.Batches() {
			require.Equal(t, original, batch.Streams()[0].Entries)
		}
		require.Len(t, write3.Batches(), 1)
		require.Equal(t, original, write3.Batches()[0].Streams()[0].Entries)
	})

	t.Run("downstream that is never replaced retries until ctx is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var calls int
		stopped := consumerFunc{consume: func(_ context.Context, _ Batch) error {
			calls++
			if calls == 5 {
				cancel()
			}
			return ErrConsumerStopped
		}}

		b := NewStreamBatcherV2(NewFanoutV2([]LogsReceiver{NewLogsReceiverV2(stopped, "stopped")}), labels, 10)
		addAll(b, original)

		// Callers tell a ctx error apart by checking ctx.Err(), not the
		// returned error, which is the last error from the consumer.
		require.Error(t, b.Flush(ctx))
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, 5, calls)
	})

	t.Run("other errors are not retried", func(t *testing.T) {
		var calls int
		failing := consumerFunc{consume: func(_ context.Context, _ Batch) error {
			calls++
			return errors.New("consume failed")
		}}

		b := NewStreamBatcherV2(failing, labels, 10)
		addAll(b, original)
		require.Error(t, b.Flush(t.Context()))
		require.Equal(t, 1, calls)
	})
}

// BenchmarkStreamBatcherV2 measures one Add of flushSize entries and a Flush
// through a FanoutV2 with one consumer. "retry" makes the consumer report
// ErrConsumerStopped once per batch, so every batch is sent twice.
func BenchmarkStreamBatcherV2(b *testing.B) {
	const flushSize = 100
	labels := model.LabelSet{"service_name": "app", "filename": "/var/log/app.log"}
	entry := push.Entry{Timestamp: time.Unix(1, 0), Line: `ts=2026-10-23T12:00:00Z level=info msg="benchmark line"`}

	for _, tc := range []struct {
		name  string
		retry bool
	}{
		{name: "happy"},
		{name: "retry", retry: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var calls int
			consumer := consumerFunc{consume: func(_ context.Context, _ Batch) error {
				calls++
				if tc.retry && calls%2 == 1 {
					return ErrConsumerStopped
				}
				return nil
			}}
			batcher := NewStreamBatcherV2(NewFanoutV2([]LogsReceiver{NewLogsReceiverV2(consumer, "bench")}), labels, flushSize)
			ctx := b.Context()

			b.ReportAllocs()
			for b.Loop() {
				for range flushSize {
					batcher.Add(entry)
				}
				if err := batcher.Flush(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
