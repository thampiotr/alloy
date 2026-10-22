# Loki pipeline V2 component rules

Each component's V2 path is a separate branch behind the `loki.pipeline-v2.enabled` flag, so v1 code is
not changed.

## Rules

1. **`Consume` is a plain call.** Applies to components that export a Loki receiver: `loki.echo`,
   `loki.relabel`, `loki.enrich`, `loki.secretfilter`, `loki.process`, `loki.write`,
   `otelcol.receiver.loki` and `database_observability.postgres`.
   - It works from the moment `New` returns until the component stops.
   - It waits only on downstream backpressure. Cancelling `ctx` or stopping the component ends the wait.
   - It returns `nil`, a `ctx` error or `ErrConsumerStopped`. The component retries its own failures.
     Entries it drops on purpose are counted in its metrics, and `Consume` returns `nil`.
   - After the component stops, new and in-flight calls return `ErrConsumerStopped`. A component that
     buffers entries stops in this order: mark itself stopped, wait for in-flight calls to return, then
     flush the buffer.
   - It never holds a lock while calling downstream.
2. **`Update` never waits on data.**
   - It switches `FanoutV2` to the new downstream first, before anything that can fail.
   - A source restarts its workers only when the relevant config changed. It cancels their `ctx`, then
     waits for them to exit.
3. **Sources handle `Consume` errors.** To tell a `ctx` error apart, check `ctx.Err() != nil`, not the
   returned error.
   - On a `ctx` error, the entries may not have been delivered. Don't advance the position; send them
     again when the worker restarts. A push source returns the error to its client, so the client
     retries. A source that can't replay, such as UDP syslog, drops the entries and logs it.
   - Any other error is likely a bug in pipeline V2. Log it clearly, drop the entries to unblock the
     pipeline, and advance.
   - Delivery is at-least-once: a downstream component can get the same entries twice.
4. **`FanoutV2` delivers to the current downstream.** If a child returns `ErrConsumerStopped`, deliver the
   batch to the children added since the snapshot. If the children haven't changed, return the error.
   Components only need to use `FanoutV2`.

## Issues these rules fix

1. **New downstream not started yet (rule 1).** A reload replaces `loki.write "old"` with
   `loki.write "new"`. Upstreams are pointed at new components before those components are started, so
   `loki.process` forwards to `"new"` before `"new"` runs. Because `"new"` accepts `Consume` as soon as
   `New` returns, nothing blocks.
2. **`Update` stuck behind a blocked `Consume` (rule 1, no locks).** The Loki endpoint is down and
   `loki.write` has `block_on_overflow = true`, so `loki.process` is blocked inside a downstream call. The
   user reloads to fix the endpoint. If `loki.process` held its read lock during that call, its `Update`
   would wait for the write lock forever, and the fix in `loki.write` would never be applied.
3. **Reload deadlock when a source restarts (rules 1 and 2).** From
   [grafana/alloy#7306](https://github.com/grafana/alloy/issues/7306) and the `loki.source.cloudflare`
   discussion: the source's `Update` waits for its tailer, the tailer waits for downstream, and downstream
   waits for a component that only starts after the reload. In V2, `Update` cancels the tailer's `ctx`,
   and the tailer's `Consume` returns straight away.
4. **Failed `Update` keeps sending to a removed component (rule 2).** The `reroute_when_start_fails`
   scenario from [grafana/alloy#7438](https://github.com/grafana/alloy/pull/7438): `Update` fails, but
   the reload still stops the components it removed. Switching `FanoutV2` first means the component
   already sends to the new downstream.
5. **Swapped or renamed component loses a batch (rule 4).** `file -> process -> write` becomes
   `file -> relabel -> write`. A batch that `file` sent to `process` just before `process` stopped is
   rejected with `ErrConsumerStopped`, and `FanoutV2` delivers it to `relabel` instead. Renaming
   `process "a"` to `process "b"` is the same case.
6. **A removed processor and its removed downstream (rules 1 and 4).** The `reroute_chain` scenario from
   grafana/alloy#7438. A call is in flight through a removed `loki.process` to a removed `loki.write`.
   `loki.write` returns `ErrConsumerStopped`, and so does `loki.process`, because it has stopped too.
   The source's `FanoutV2` then delivers the batch to the new chain.
7. **Entries lost when a buffering component stops (rule 1, stop order).** `loki.process` multiline
   stages hold entries. If the buffer is flushed while a `Consume` is still in flight, the entry that call
   adds is never forwarded. `PipelineConsumer.Stop` already warns about this.
8. **Entries lost when a source restarts (rule 3).** `Update` cancels the tailer while a `Consume` is in
   flight. The entries may not have been delivered, so the tailer keeps its position and sends them again
   after it restarts.
9. **Deliberate drops reported as bugs (rule 1, errors).** `loki.write` with a full queue and
   `block_on_overflow = false`, or the `limit` stage in `loki.process`, drop entries on purpose. They count
   the drop and return `nil`, so the source doesn't log a pipeline V2 bug.
10. **Upstream pointing at a stopped component (rules 3 and 4).** An upstream's arguments fail to
    evaluate, so its `Update` isn't called and it keeps its old `forward_to`. The reload still stops the
    removed downstream. `FanoutV2` sees no change in its children and returns the error, and the source
    logs it and drops.

## TODO

- Enforce rule 1 in tests: Shared `lokitest` check that every component exporting a receiver runs when it's migrated (rule 1):
  - `Consume` works before `Run`;
  - after stop, `Consume` returns `ErrConsumerStopped`;
  - (if relevant) a blocked `Consume` returns once `ctx` is cancelled.

- `FanoutV2`: implement redelivery (rule 4). Today it ignores `ErrConsumerStopped`.
- `loki.source.file` tailer
  ([grafana/alloy#7455](https://github.com/grafana/alloy/pull/7455)): make the log message for
  unexpected errors clear (rule 3). The `ctx` case already doesn't advance.
- `otelcol.receiver.loki`: retry errors that can be retried, and stop holding its read lock while calling
  `logsSink.ConsumeLogs` (rule 1).
- `loki.process` `PipelineConsumer.Stop`: follow the stop order (rule 1). It has an existing TODO about
  entries blocked inside a stage.

## Out of scope (v1 limits that stay unaddressed here)

- Exclusive ports when a source is renamed.
- Retrying a failed start, and reporting it as component health.
- `Validate` for config that can never work.
- Upstreams keeping a stale `forward_to` when argument evaluation fails. Issue 10 above only makes it
  visible.
