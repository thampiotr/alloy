# ShardingConsumer decision: research handover

Prepared 2026-10-07. This branch preserves research, an interactive walkthrough,
and isolated experiments. It does not change Alloy's production pipeline.

## Start here

- Open [the interactive research page](../loki-pipeline-research.html) in a browser.
  It is a self-contained HTML file; no server or network is required for its controls.
- The six page tabs correspond to Piotr's six research tasks below.
- Read the [original reliable-pipelines proposal](../../../docs/design/4940-reliable-loki-pipelines.md).
- Baseline: Alloy main fetched on 2026-10-05, commit `92ff74120`.
  Findings describe that revision, not any subsequent main changes.
- Destination: `thampiotr/alloy`, branch `sharding-consumer-decision`.

## Piotr's input and constraints

The user first asked for a detached checkout of latest main and an understanding
of the reliable Loki pipeline proposal, `ShardingConsumer`, and the path from
`loki.source.file` through `loki.process` to `loki.write`, excluding WAL for now.
They then explicitly authorized discarding previous unrelated worktree changes.
Those Windows-service changes were discarded; they are not part of this branch.

The central idea was the user's: consider deleting `ShardingConsumer` to reduce
parts and complexity. Reuse the goroutines already reading logs in `loki.source.*`
to run the synchronous pipeline. Let each source determine its maximum parallelism;
blocking downstream calls should provide backpressure to those readers.

The requested investigation was:

1. Define the source contract and threading model clearly.
2. Check whether every source already has a reusable goroutine per stream.
3. Check whether all sources can implement that contract.
4. Research the advantages and disadvantages of both designs.
5. Identify the required trade-offs.
6. Suggest a decision.

Piotr authorized focused prototypes and tests, but explicitly asked not to run
expensive full-repository lint or test suites. They subsequently asked for a visual
HTML page with diagrams and pseudocode because the prose report was too long, and
asked that the presentation map directly to the six original tasks. The final
request was to preserve all this work on a new branch in their fork, with this handover.

## Results mapped to the six tasks

| Task | Main result | Where to continue |
| --- | --- | --- |
| 1. Contract | Sources own synchronous execution, contexts, admission, sequencing and acknowledgements. Exclusive stream ownership or explicit serialization is required. | Page tab 01, file and HTTP pseudocode. |
| 2. Inventory | All 16 registered source components were reviewed. A goroutine commonly belongs to a target, connection, partition or request rather than a Loki stream. | Tab 02 has individual source details and code links. |
| 3. Feasibility | Source-owned execution is feasible with adaptations. Lossless upstream backpressure is not universally possible, particularly for UDP and informer callbacks. | Tab 03 covers protocol and hidden-buffer boundaries. |
| 4. Comparison | Direct execution removes handoffs for natural readers. Sharding also provides serialization, a processing concurrency bound and parallelism within a multi-stream request. | Tab 04 includes scheduling illustrations and comparison table. |
| 5. Trade-offs | Decide buffered-stage acknowledgement semantics, stream-gate design, intra-input parallelism and source admission/retry policy. | Tab 05 explains the four decisions. |
| 6. Suggestion | Prefer source-owned execution; remove mandatory sharding. Make deletion conditional on covering overlapping-stream sources and retaining any justified bounded dispatch. | Tab 06 gives a proposed validation sequence. |

## Facts established from the code

- `ShardingConsumer` uses unbuffered worker channels and original-label fingerprints.
  A single-stream call avoids splitting, but still makes a worker handoff.
- Multi-stream dispatch is sequential. A busy shard can prevent submission to a
  later idle shard. Different streams that collide share a worker's backpressure.
- The sharder waits for accepted downstream calls to return. It does not make
  cancellation work when a downstream consumer ignores its context.
- The new function-based consumer and stage pipeline exist, but the file, process
  and write components at this baseline still connect through channels.
- A file tailer has a natural sequential reader. Its tracked offset currently
  advances after channel handoff; the migration needs acknowledged progress,
  retained retry input and bounded batching.
- Docker's stdout and stderr readers, Kubernetes targets and Kafka partitions can
  emit identical public labels. Ownership of a reader does not prove stream exclusivity.
- API, Heroku, Firehose and GCP push use concurrent HTTP handlers. GCP pull uses
  concurrent Pub/Sub callbacks. These can overlap on the same Loki stream.
- Cloudflare's concurrent time-range fetches emit the same configured labels.
  Serializing calls does not by itself restore chronological range ordering.
- Journal and Windows Event Log have sequential reading loops. GELF has a single
  UDP processing loop. Increasing their intra-input processing parallelism requires
  explicit dispatch somewhere.
- Kubernetes event informer callbacks can block behind a client-go unbounded
  notification buffer. A bounded local queue that blocks the callback when full
  does not solve that hidden backlog.
- Write's memory queues and HTTP sender workers serve a separate purpose and remain
  necessary. Without WAL, successful acceptance is not durable backend delivery.

## Suggested contract and important limits

One source instance should prevent overlapping pipeline execution for identical
original stream labels, including tenant identity. An exclusive reader can satisfy
this naturally; multiplexed producers may need a shared cancellable gate.
Sequential input is preserved, but no timestamp sorting, global order across
source instances, or ordering after downstream label merges is promised.

Sources need both an active-processing bound and a retained-input budget. Waiting
goroutines still retain data. HTTP admission should precede expensive decoding,
and processing needs its own deadline rather than relying on response write timeout.

Ordered readers should retry unresolved input before processing later batches and
must not commit past failure. Retries can duplicate already-accepted fan-out branches.
Keep pristine retry input because downstream processing may mutate a batch.
All consumers must support concurrent callers, including callers from other sources.

During shutdown or replacement, stop new calls, cancel or complete in-flight work,
wait for callers to stop using borrowed data, then flush buffered stages and drain
write queues. Source replacement must not accidentally create two owners of a stream.

## Unresolved design questions

The most important acknowledgement issue is independent of sharding. The new CRI
stage can return success while holding partial lines; multiline needs future input
to finish a record. A sole reader cannot wait for that future record to be accepted
by write before supplying the continuation lines.

Two possible contracts remain: count acceptance into bounded stage buffers as
success, accepting the crash-loss window; or separate submission from completion
and maintain bounded in-flight input plus contiguous acknowledged positions.
The latter is stronger and more complex. No choice was approved by the user.

Other open choices:

- Fixed striped gates are simple and bounded, but retain hash collisions. Exact
  stream gates need registry lifecycle and memory controls.
- A direct call processes a multi-stream request sequentially unless the source
  adds bounded dispatch. No representative throughput benchmark has been run.
- A shared helper avoids reproducing subtly different concurrency mechanisms in
  every source. That helper's API has not been designed or implemented for production.

The recommendation to adopt source-owned execution is the assistant's research
suggestion, not a final user-approved architectural decision. The user proposed
investigating deletion, not implementing it immediately.

## Artifacts and focused verification

- `../loki-pipeline-research.html`: offline visual walkthrough with six task tabs,
  backpressure toggle, source inventory, scheduling scenarios and pseudocode.
- `experiment/testdata/consumer_sharding.go`: unchanged copy from the baseline repository.
- `experiment/testdata/model.go` and its `prometheus-common/model/model.go`: simplified batch
  and label types for isolated scheduling checks; not production substitutes.
- `experiment/testdata/scheduling_test.go`: six deterministic tests using `testing/synctest`.
- `experiment/run.sh`: creates a temporary standalone module from the fixtures,
  runs focused tests and removes it afterwards. The `.fixture` module manifests
  avoid adding an active module or modifying Alloy's dependency graph. Go also
  skips the `testdata` directory during normal package discovery.
- `validation/verify-page.py` and `check-page.cjs`: HTML structure checks, JavaScript
  syntax checking and a lightweight non-browser DOM harness for control behavior.

From the repository root, with Go 1.25+ and race-detector support, Python 3 and Node:

```sh
sh output/sharding-consumer-decision/experiment/run.sh
python3 output/sharding-consumer-decision/validation/verify-page.py
```

The six scheduling checks passed under `-race` during the original research. They
cover same-stream overtaking, sequential reader backpressure, hash collisions,
head-of-line blocking in multi-stream dispatch, parallelism within a multi-stream
call, and cancellation after a worker has accepted a call. They do not establish
production throughput, full pipeline correctness or all stages' concurrency safety.

Page structure and interaction checks also passed. Browser security policy blocked
the local-file visual preview, so the page has not been visually verified in a
browser by the agent. Open the HTML manually to inspect desktop/mobile layout.
No full-repository lint or tests were run, per the user's constraint.

## Next work

1. Agree the ordering scope and buffered-stage acknowledgement contract with Piotr.
2. Prototype the file path with bounded batches and acknowledged offsets.
3. Test full write queues, cancellation, rotation, retries and reader replacement.
4. Exercise overlapping HTTP streams, partition collisions and Cloudflare emission.
5. Measure representative throughput, allocations, tail latency and congestion
   before deciding whether optional intra-input dispatch should survive.
6. Delete `ShardingConsumer` only when each source's replacement policy is covered.

Keep subsequent explanations concise and visual. This handover is repository
documentation for continuing the work. Any `HUMAN ONLY` PR content or review
discussion must be written by the human in their own words under
[Alloy's GenAI policy](../../../docs/developer/genai.md).
