package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/alloy/internal/component/common/loki"
	"github.com/grafana/alloy/internal/component/loki/source/internal/positions"
	"github.com/grafana/alloy/internal/runtime/logging"
)

const benchLinesPerOp = 10_000

// BenchmarkTailer reads a file of benchLinesPerOp lines through a tailer in
// every op and reports allocations per line read, so the result can be
// compared with main. "v2_retry" makes the consumer report ErrConsumerStopped
// once for every batch, so every batch is sent twice.
func BenchmarkTailer(b *testing.B) {
	path := writeBenchFile(b, benchLinesPerOp)

	b.Run("v1", func(b *testing.B) {
		runTailerBench(b, path, func(ctx context.Context, c *benchCounter) func(model.LabelSet) loki.StreamBatcher {
			recv := loki.NewLogsReceiver()
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-recv.Chan():
						c.add(1)
					}
				}
			}()
			return func(labels model.LabelSet) loki.StreamBatcher {
				return loki.NewStreamBatcherV1(recv, labels)
			}
		})
	})

	for _, tc := range []struct {
		name  string
		retry bool
	}{
		{name: "v2"},
		{name: "v2_retry", retry: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			runTailerBench(b, path, func(_ context.Context, c *benchCounter) func(model.LabelSet) loki.StreamBatcher {
				var calls int
				consumer := benchConsumer(func(_ context.Context, batch loki.Batch) error {
					calls++
					if tc.retry && calls%2 == 1 {
						return loki.ErrConsumerStopped
					}
					c.add(batch.EntryLen())
					return nil
				})
				fanout := loki.NewFanoutV2([]loki.LogsReceiver{loki.NewLogsReceiverV2(consumer, "bench")})
				return func(labels model.LabelSet) loki.StreamBatcher {
					return loki.NewStreamBatcherV2(fanout, labels, 100)
				}
			})
		})
	}
}

func runTailerBench(b *testing.B, path string, setup func(ctx context.Context, c *benchCounter) func(model.LabelSet) loki.StreamBatcher) {
	metrics := newMetrics(nil)
	logger := logging.NewSlogNop()

	var (
		before, after runtime.MemStats
		ops           int
	)
	runtime.GC()
	runtime.ReadMemStats(&before)

	b.ReportAllocs()
	for b.Loop() {
		ops++
		ctx, cancel := context.WithCancel(b.Context())
		c := &benchCounter{target: benchLinesPerOp, done: make(chan struct{})}

		tailer := newTailer(metrics, logger, setup(ctx, c), positions.NewNop(), func() bool { return true }, sourceOptions{
			path:   path,
			labels: model.LabelSet{"service_name": "app"},
			fileWatch: FileWatch{
				MinPollFrequency: 10 * time.Millisecond,
				MaxPollFrequency: 10 * time.Millisecond,
			},
		})

		exited := make(chan struct{})
		go func() {
			tailer.Run(ctx)
			close(exited)
		}()

		<-c.done
		cancel()
		<-exited
	}

	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.Mallocs-before.Mallocs)/float64(ops*benchLinesPerOp), "allocs/line")
}

type benchCounter struct {
	n      atomic.Int64
	target int64
	done   chan struct{}
}

func (c *benchCounter) add(n int) {
	if c.n.Add(int64(n)) == c.target {
		close(c.done)
	}
}

type benchConsumer func(context.Context, loki.Batch) error

func (f benchConsumer) Consume(ctx context.Context, batch loki.Batch) error { return f(ctx, batch) }

func writeBenchFile(b *testing.B, lines int) string {
	b.Helper()

	var sb strings.Builder
	for i := range lines {
		fmt.Fprintf(&sb, "ts=2026-10-23T12:00:00Z level=info msg=\"benchmark line\" n=%d\n", i)
	}

	path := filepath.Join(b.TempDir(), "bench.log")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}
