# Plan: move Docker integration tests to the right test level

## Goal

Move each test in `integration-tests/docker` to the level where it belongs, as defined in
[docs/developer/writing-tests.md](docs/developer/writing-tests.md).

## The triage rule

Answer three questions about each test, in order.

**1. Does the coverage already exist somewhere cheaper?**

Check the component's unit tests and the existing pipeline tests before writing anything. Several
Docker tests turn out to duplicate coverage that already exists, sometimes far more thoroughly. When
that happens, the answer is to delete the Docker test, or to extend the cheaper test, not to
relocate it.

**2. Is the assertion about what Alloy produces, or about what a third-party system does with it?**

- What Alloy produces -> pipeline test in `internal/pipelinetest`. Alloy runs in process. A file, a
  socket or a local HTTP mock is not a third-party system.
- What a third-party system does -> integration test in `integration-tests/k8s`. Also use this level
  for engine and configuration features, such as the Helm chart, a CLI flag or Alloy's own HTTP API.

Writing to Mimir or Loki does not by itself make a test an integration test. The pipeline sink
absorbs Loki pushes, Prometheus remote write v1 and v2, and direct appends, so it replaces both
backends for anything that only checks Alloy's own output.

**3. Is this essential enough to deserve more than one level?**

Question 1 asks whether cheaper coverage exists. This one asks whether cheaper coverage is
*sufficient*. For the most essential paths it is not, and an integration test earns its keep even
when unit and pipeline tests already pass.

Reading a log file is the worked example. Unit tests cover the read, `loki-pipeline-file` covers it
inside a pipeline, and both run the component as library code in the test process. Neither reaches
the deployed path: a chart-managed DaemonSet tailing container logs from the node through the
`/var/log` hostPath, with targets built by `discovery.kubernetes`. That gap is why `loki-file` moved
to k8s rather than being deleted.

Apply this sparingly. "Essential" means a path that most users depend on and that would be a serious
regression if broken. It is not a licence to keep every test at every level.

### A note on what to keep when converting

When a Docker test does survive the three questions, port the assertions that belong at the target
level and drop the rest. The push-based log receivers are the common case: their unit tests already
drive the real listener in process, so a pipeline test should not re-prove the intake. What it should
prove is the composition, which is the part no unit test covers: structured metadata surviving to
the write, or a label that must not be indexed, rather than that an HTTP POST is accepted.

## YAML is the product, not just the test format

The declarative YAML format is intended to become a user-facing feature, so Alloy users can write
their own pipeline tests. That changes how we do this work.

- **Every capability in the table below is product surface.** Design each field for a user writing a
  test for their own pipeline, not for the one Docker test that motivated it. Name fields, document
  them, and pick defaults accordingly.
- **Reach for a declarative capability first.** If a Docker test needs something the schema cannot
  express, the first question is whether users would want to express it too. Usually they would, and
  then the answer is a schema extension rather than an escape hatch.
- **Imperative mode is a second mode, not a fallback.** It exists side by side with YAML for tests
  whose essence is timing and orchestration during the run. One test on this plan needs it. Gate
  every future use on the same test: could a user express this declaratively? If yes, extend the
  schema instead.

## Method

One test per pull request. Each pull request brings the capability that its test needs.

**Audit before you convert.** Run the three triage questions against the actual code first, and
record the answer. The audit column in the track A table below holds the result. Anything marked
`todo` has not been audited, so treat its row as a guess, not a decision.

We do not build capabilities up front. The first test that needs one pays for it. Later tests reuse
it. The order below is chosen so that each step adds at most one new capability.

## Scope and progress

Thirty-three Docker tests existed when this work started. `integration-tests/docker/tests` holds 23
on main today.

| State | Count |
| --- | --- |
| Done | 10 |
| Open | 1 |
| Track A, pending | 9 |
| Track B, pending | 8 |
| Track C, stays on Docker | 3 |
| Track D, decide later | 2 |

Done so far:

| Docker test | New home | Level | PR |
| --- | --- | --- | --- |
| `static` | `prometheus-exporter-static` | pipeline | #6959 |
| `loki-file` | `loki-source-file` | k8s | #7187 |
| `graphql` | `graphql` | k8s | #7202 |
| `otlp-metadata` | `otlp-metadata` | k8s | #7215 |
| `redis` | `prometheus-exporter-redis` | k8s | #7219 |
| `database-observability-mysql` | `database-observability-mysql` | k8s | #7220 |
| `database-observability-postgres` | `database-observability-postgres` | k8s | #7221 |
| `prom-metadata` | `prometheus-remote-write-v2-metadata` | pipeline | #7225 |
| `loki-enrich` | `loki-enrich` | pipeline | #7299 |
| `loki-api` | `loki-source-api` | k8s | #7312 |

Open: `scrape-prom-metrics`, folded into `otlp-metadata` renamed to `prometheus-write-paths`, in #7300.

Stop after tracks A and B. Do not start track C or D work as part of this plan.

## Capabilities

Each capability is listed against the first test that needs it. C1 to C5 are declarative, so they
are all user-facing features. C6 is the imperative escape hatch.

| ID | Capability | First needed by |
| --- | --- | --- |
| C1 | `inputs.prometheus`, feeding metric samples into a receiver. Mirrors `inputs.loki`. | A6 `prom-enrich` |
| C2 | `assert.prometheus` metadata matching, for type, help and unit. **Done in #7225:** the sink keeps metadata and `match.metadata` asserts it. | A7 `prom-metadata` |
| C3 | `inputs.http`, sending HTTP requests to a component's own listener. Covers `loki.source.api`, `loki.source.heroku`, `loki.source.awsfirehose`, `loki.source.gcplog` push, `prometheus.receive_http`, `faro.receiver` and OTLP over HTTP. Ports are hardcoded. See note A. | A9 `loki-heroku`, if the HTTP push receivers stay in track A. See note N. |
| C4 | `inputs.tcp` and `inputs.udp`, sending raw bytes or lines to a listener. Covers `loki.source.syslog` and `loki.source.gelf`. | A11 `loki-gelf` |
| C5 | `mocks.http`, a stub upstream the config can point at, with canned responses per path. Covers any component that pulls from an HTTP API, and any component that probes an endpoint. | A13 `blackbox` |
| C6 | Imperative mode. A Go-authored entry point beside the YAML runner, for tests that must manipulate state while Alloy runs. `harness.NewAlloy` already exists, so this is mostly a runner and a convention. | A15 `loki-file-rotation` |

Already landed:

- [#6959](https://github.com/grafana/alloy/pull/6959): Prometheus receiver export, sample capture,
  Prometheus assertions, and the real HTTP service in the harness so `prometheus.exporter.*`
  components can be scraped.
- [#7117](https://github.com/grafana/alloy/pull/7117): `pipelinetest.prometheus.url`, a remote write
  endpoint accepting both PRW v1 and v2.
- [#7225](https://github.com/grafana/alloy/pull/7225): Prometheus metadata in the sink and in
  `assert.prometheus`, which is C2.

One gap remains, and nothing in track A needs it yet:

- **OTLP signals in the sink.** This would let the Alloy-side half of B11
  `otlp-metrics-default-engine` move down a level. The write-path checks in
  `prometheus-write-paths` stay at k8s level on purpose, because they are about wire compatibility
  with a real backend. Do it as its own change, not inside a migration.

## Track A: convert to a pipeline test

The assertion is about Alloy's own output. Alloy runs in process, so these get faster and simpler,
not just relocated.

The audit column records triage question 1. `done` means the existing unit and pipeline coverage
was checked and the row reflects it. `todo` means it has not been, so treat the row as a guess.

| # | Test | Capability | Effort | Audit | Done |
| --- | --- | --- | --- | --- | --- |
| A1 | `static` | Prometheus receiver, assertions, and the real HTTP service in the harness. | small | done | **merged, #6959** |
| A2 | `loki-file` | **Moved to track B.** See B0. | small | done | **merged, #7187** |
| A3 | `loki-file-compression` | **Moved to unit tests.** See note L. | small | done | no |
| A4 | `loki-enrich` | None. `loki.enrich` fed from `inputs.loki`. See notes B and M. | small | done | **merged, #7299** |
| A5 | `scrape-prom-metrics` | **Moved to track B.** See B14 and note C. | small | done | **open, #7300** |
| A6 | `prom-enrich` | C1. The last Docker test using `prom-gen`. See note M. | medium | done | no |
| A7 | `prom-metadata` | C2. | medium | done | **merged, #7225** |
| A8 | `loki-api` | **Moved to track B.** See B15 and note N. | medium | done | **merged, #7312** |
| A9 | `loki-heroku` | C3, or move to track B like A8. See note N. | small | done | no |
| A10 | `loki-firehose` | C3, or move to track B like A8. See note N. | small | done | no |
| A11 | `loki-gelf` | C4. See notes D and N. | medium | done | no |
| A12 | `loki-syslog` | None. Reuses C4. See notes D, E and N. | small | done | no |
| A13 | `blackbox` | C5. See note O. | medium | done | no |
| A14 | `loki-cloudflare` | None. Reuses C5. See note N. | small | done | no |
| A15 | `loki-file-rotation` | C6. See note F. | medium | todo | no |

## Track B: move to a k8s integration test

The assertion is about a third-party system, or about an engine or configuration feature.

| # | Test | Why this level | Capability this step adds | Effort | Done |
| --- | --- | --- | --- | --- | --- |
| B0 | `loki-file` | Essential path. Unit and pipeline tests cover the read, neither covers the deployed volume mount. Triage question 3. | None. Tails node logs via `discovery.kubernetes` and `alloy.mounts.varlog`, with the existing `log-gen` dep. | small | **merged, #7187** |
| B1 | `graphql` | Alloy's own HTTP API, behind a CLI flag. | `alloy.extraArgs`, plus port-forwarding on the Alloy dep, since generalised to `ForwardPorts` in #7312. | small | **merged, #7202** |
| B2 | `redis` | Real Redis. | Redis dep, with the seed as a Job that `Install` waits on. | medium | **merged, #7219** |
| B3 | `snmp` | Real SNMP agent. | snmpsim dep. UDP stays inside the cluster. | medium | no |
| B4 | `database-observability-mysql` | Real MySQL. | MySQL dep seeded via an init ConfigMap, plus `loki.QueryLogsPresent`. See note P. | medium | **merged, #7220** |
| B5 | `database-observability-postgres` | Real Postgres. | Postgres dep with `pg_stat_statements` preloaded and a `monitoring_user` role. | medium | **merged, #7221** |
| B6 | `kafka` | Real Kafka. | Kafka dep, plus a message generator for consumer group metrics. See notes G and H. | medium | no |
| B7 | `loki-kafka` | Real Kafka. | An in-cluster producer. See note H. | medium | no |
| B8 | `loki-gcplog` | Pub/Sub emulator for the pull path. | Pub/Sub emulator dep. The push path can use `ForwardPorts`, as `loki-source-api` does. | medium | no |
| B9 | `otlp-metrics-otel-engine` | The standalone `/bin/otelcol` binary. | Reuses B10's `otel-gen`, but needs a way to run `/bin/otelcol`. See note Q. | medium | no |
| B10 | `otlp-metrics-otel-engine-using-alloyengine-extension` | The alloyengine extension. | An `otel-gen` fixture image. `deps.AlloyOtel` already fits this test exactly. See note Q. | medium | no |
| B11 | `otlp-metrics-default-engine` | Real Mimir and Tempo. | Tempo dep and a traces assertion helper. See note I. | medium | no |
| B12 | `otlp-metadata` | Mimir's OTLP endpoint and metadata API. | `mimir.QueryHistograms`, and a cardinality check in `QueryMetadata`. See note J. Renamed and extended by B14. | small | **merged, #7215** |
| B13 | `unix` | A pinned Linux kernel. See note K. | Host access and a textfile directory. | medium | no |
| B14 | `scrape-prom-metrics` | Remote write v1, v2 and OTLP against a real backend, where wire compatibility is what matters. | Renames B12's test to `prometheus-write-paths` and adds both remote write paths. See note C. | small | **open, #7300** |
| B15 | `loki-api` | A push receiver exposed through a Service via `alloy.extraPorts`. The intake itself is unit-tested. | `ForwardPorts` on the Alloy dep, and `loki.QueryLabelsNotIndexed`. | medium | **merged, #7312** |

## Track C: stay on Docker

| Test | Reason |
| --- | --- |
| `loki-docker` | `discovery.docker` and `loki.source.docker` read `/var/run/docker.sock`. Docker-specific by definition. |
| `oracledb` | `test.yaml` layers Oracle Instant Client on the Alloy image. Neither harness can do per-test Alloy images. Also two heavy Oracle instances and a 15m timeout. |
| `loki-azure-event-hubs` | The compose file needs hand-tuned entrypoint overrides to work around the Kafka image exporting an empty `KAFKA_SSL_KEYSTORE_PASSWORD`, plus a cert-generation init container. This is the "very convenient Docker option" case. |

## Track D: decide later

| Test | Effort | What to decide |
| --- | --- | --- |
| `beyla` | high | The chart supports `hostPID` and `privileged`, and Beyla on k8s is the primary real deployment, so the value is high. The risk is also high: eBPF on kind nodes needs the right kernel, plus `debugfs` and `bpf` mounts. Prove eBPF works in kind first. Keep Docker until then. |
| `beyla-java` | high | Adds the OBI Java agent injector attaching into a JVM in a separate container. Do this only after `beyla` works. |

## Definition of done for one step

Nothing is ready for human review until every item below holds.

1. **Run the test.** Not a skip-check, an actual run. See "Running the tests" below.
2. **Break one assertion on purpose and confirm it fails with a useful message.** A test that passes
   for the wrong reason is worse than no test. This has caught real gaps: a bucket-parsing helper
   returned zeroed structs while `NotEmpty` still passed.
3. **Diff the Docker helper's whole assertion set, not just the test body.** The Docker `common`
   helpers assert far more than they appear to. `MimirMetricsTest` checks series names *and* per
   metric sample data *and* native histogram count, sum and buckets. `MimirMetadataTest` also
   requires exactly one metadata entry per metric. Porting only the visible call loses coverage
   silently.
4. Any new declarative capability is documented for users, not just used. It is product surface.
5. Any new k8s dependency lives in `integration-tests/k8s/deps` and implements
   `harness.Dependency`.
6. The Docker copy of the test is deleted in the same pull request.
7. Compose services in `integration-tests/docker/docker-compose.yaml` that lose their last consumer
   are deleted in the same pull request.
8. Scoped lint passes: `golangci-lint run ./integration-tests/k8s/...` or
   `./internal/pipelinetest/...`. Whole-repo `make lint` is slow and rarely tells you anything the
   scoped run does not.
9. **Open the PR as a draft, get a bot review, fix what is real, and get CI green.** See "Before
   handing over for human review" below.

## Running the tests

Static checks are not enough. `go vet`, `gofmt`, `alloy validate`, `helm template` and a
skip-without-runner `go test` all passed on a config whose relabel rule silently produced the wrong
path, and CI caught it instead.

Track A:

```sh
go test -count=1 -run 'TestPipelines/<name>' ./internal/pipelinetest/
```

Track B:

```sh
make integration-test-k8s RUN_ARGS='--package ./integration-tests/k8s/tests/<name> --reuse-cluster --skip-image-builds'
```

Notes on the Track B flags and environment:

- `--reuse-cluster` keeps the kind cluster between runs, which turns a ~3 minute cycle into ~30
  seconds. Use it for every iteration after the first.
- `--skip-image-builds` uses the `grafana/alloy:latest` already in the local docker daemon. Much
  faster, but remember the image may be stale, so CI is still the first run against current code.
- The test namespace is deleted on cleanup, so inspect the cluster **while the test is running**, not
  after. `kubectl --kubeconfig integration-tests/k8s/.kube/kubeconfig`, or
  `docker exec <kind-node>` for anything on the node filesystem.
- Alloy has no `curl` or `wget`. To reach its HTTP API, `kubectl port-forward` to the pod and query
  `/api/v0/web/components/<id>` from the host. That is how to see a component's resolved targets and
  exports, which is usually the fastest way to find a config bug.
- If a docker build hangs on `resolve image config`, it is the credential helper, not the network.
  `DOCKER_CONFIG=<dir-with-empty-config.json>` sidesteps it without touching the real config.
- Run every test that shares a helper you changed. Changing `mimir.QueryMetadata` meant re-running
  `prometheus-exporter-mssql`, `prometheus-operator` and `prometheus-exporter-cadvisor`.
- After the Docker daemon restarts, the reused kind node needs a few seconds before its API server
  answers. A run started too early fails on the first `kubectl apply` with `failed to download
  openapi`. Wait for `kubectl get nodes` to report `Ready`.

Track A has one trap of its own: the first `go test ./internal/pipelinetest/` after switching branch
can take several minutes, almost all of it linking, because the test binary includes every Alloy
component. Later runs reuse the build cache and take seconds.

## Before handing over for human review

Do this while the PR is still a draft, so the first thing a human sees is already clean.

1. Open the PR as a **draft**.
2. Request a **Cursor Bugbot** review first, by posting the bare trigger:
   `gh api -X POST repos/grafana/alloy/issues/<n>/comments -f body='@cursor review'`.
   It replies as a review from `cursor[bot]`, typically within about 10 minutes. Bugbot also runs as
   an automatic check, but not on drafts, which is why the trigger is needed here. Keep the comment
   to the trigger string alone: GitHub comment text is human-owned.

   Fall back to Copilot only if Bugbot finds nothing useful:
   `gh api -X POST repos/grafana/alloy/pulls/<n>/requested_reviewers -f "reviewers[]=Copilot"`.
   That API call is unreliable, so confirm via the timeline rather than `requested_reviewers`, which
   empties as soon as the bot picks the request up:
   `gh api repos/grafana/alloy/issues/<n>/timeline --jq '.[] | select(.event=="review_requested") | .requested_reviewer.login'`.
   When it silently no-ops, ask the human to click the re-request arrow instead of retrying.

   When polling for either bot, poll the **reviews** endpoint, not just comments and checks. Both
   post their findings as a review body, and a poll that only watches comments reports nothing while
   a review is sitting there. Allow at least 15 minutes.
   A local review of the diff is worth running as well. On `loki-enrich` it caught a missing
   total-count assertion that Bugbot passed.
3. Read the review properly. Copilot puts findings in **inline comments**
   (`gh api repos/grafana/alloy/pulls/<n>/comments`) and a summary headline in the **review body**
   (`gh api repos/grafana/alloy/pulls/<n>/reviews`). It can report "Findings: None" inline and still
   raise something real in the headline, so check both.
4. **Verify each finding against the source before acting on it.** Most findings on these
   migrations were real: two coverage regressions, a dependency leak, an HTTP client with no timeout,
   an unescaped LogQL selector and a Postgres readiness probe. Three were wrong, all confidently
   worded and two marked high severity: `MYSQL_ROOT_HOST`, image digest pinning and a missing
   `USE testdb`. Each was settled by reading the actual source, not by reasoning about it: the image
   entrypoint via
   `docker run --rm --entrypoint sh <image> -c 'grep ... /usr/local/bin/docker-entrypoint.sh'`, and
   `.github/renovate.json5` for which managers exist. A passing test is corroboration, not proof, so
   pair it with evidence.
5. Fix what is real in code, and report rejections to the human with the evidence. Do not reply on
   GitHub: review replies are human-owned under
   [docs/developer/genai.md](docs/developer/genai.md).
6. Wait for CI and get it green. Distinguish real failures from flakes before changing anything.
   Two known flakes, both fixed by `gh run rerun <id> --failed`: a `proxy.golang.org` stream error
   during `lint / Lint Go`, and `run_tests` failing in `loki-kafka` or `loki-azure-event-hubs` with
   `not the leader for some partition`.
7. Only then report the PR as ready, saying what was run and what was verified.

Record the newest review id before triggering, so a poll can distinguish a new review from the old
one. Note which commit a review covers: Bugbot names it in its footer, and a clean result on an
earlier commit says nothing about later pushes.

## Translation checklist: Docker to pipeline test

| Item | Docker | Pipeline test |
| --- | --- | --- |
| Layout | `tests/<name>/config.alloy` and `*_test.go` | `internal/pipelinetest/tests/<name>/test.yaml` |
| Alloy process | container | in process, via `harness.NewAlloy` |
| Loki sink | `url = "http://loki:3100/..."` | `url = pipelinetest.loki.url` |
| Loki receiver | n/a | `pipelinetest.loki.receiver` |
| Metrics sink, remote write | `url = "http://mimir:9009/api/v1/push"` | `url = pipelinetest.prometheus.url` |
| Metrics sink, direct | n/a | `forward_to = [pipelinetest.prometheus.receiver]` |
| Test name label | `test_name` external label | not needed. Each test gets its own Alloy runtime. |
| Assertions | Go code querying Loki or Mimir | `assert.loki` or `assert.prometheus` in the YAML |
| Metadata | `common.MimirMetadataTest` | `match.metadata` on an `assert.prometheus` entry |
| Timing | scrape intervals and query retries | the harness retries assertions for 30s |

Three things to get right when translating:

- **Prefer `contains` over `count`** when a component emits repeatedly. A 1s scrape interval over the
  assertion window produces several rounds of samples, so counts are flaky.
- **Add an unmatched total `count`** when the input is fixed. Label-scoped counts cannot see entries
  emitted under any other label set, so a component that duplicated or rewrote an entry would still
  pass.
- **Use fixed timestamps.** The Docker tests generate `time.Now()` values because Loki rejects
  entries too far in the past. The sink has no such limit, so fixed timestamps are both valid and
  deterministic. See note E for the one case where this needs care.

## Translation checklist: Docker to k8s test

| Item | Docker | Kubernetes |
| --- | --- | --- |
| Test name label | `test_name` | `alloy_test_name` (see `deps/shared.go`) |
| Build tag | `//go:build alloyintegrationtests` | none. `harness.Setup` skips without the runner |
| Go package | `package main` | package named after the directory |
| Layout | `tests/<name>/config.alloy` | `tests/<name>/config/config.alloy`, optional `config/alloy-values.yaml`, and `k8s_test.go` |
| Metric names | `common.MimirMetricsTest` | `mimir.QueryMetrics` |
| Sample data | `common.MimirMetricsTest` | `mimir.QueryPositive`, and `mimir.QueryHistograms` for native histograms |
| Metadata assertion | `common.MimirMetadataTest` | `mimir.QueryMetadata` |
| Log assertion, fixed count | `common.AssertLogsPresent` | `loki.QueryLogs` |
| Log assertion, varying count | `common.LogQuery` per label | `loki.QueryLogsPresent` |
| Unindexed labels | `common.AssertLabelsNotIndexed` | `loki.QueryLabelsNotIndexed` |
| Pushing into Alloy | host port in `test.yaml` | `alloy.extraPorts` in the values file, `ForwardPorts` on the Alloy dep, then `alloy.Endpoint(port, path)` |
| Alloy flags | command line | `alloy.stabilityLevel` or `alloy.extraArgs` in the values file |

The k8s manifests use the same Service names and ports as the compose services: `mimir:9009`,
`loki:3100` and `prom-gen:9001`. Most `config.alloy` files need no endpoint edits.

**Set `scrape_interval` explicitly.** `prometheus.scrape` defaults to 60s, the same as the harness
assertion timeout, so a config relying on the default passes or fails depending on scrape phase. The
Docker suite hid this behind a 15m test timeout.

## Notes

**A. Port allocation: decided, keep hardcoded ports.** Copy the listen ports straight from the
Docker test and accept the collision risk. Do not build symbolic port references.

The risk is unchanged by the move. The remaining Docker tests already bind these ports on the host:
1516, 1517, 12201, and 51893, 51894, 51898, 51899. A pipeline test binds the same ports on the same
host. If a collision ever bites, fix it then.

What makes "fix it then" cheap is a harness change, listed under follow-ups: `harness.NewAlloy`
waits for `ctrl.LoadComplete()` but never checks component health. A component that fails to bind
its port therefore surfaces as a 30s assertion timeout saying "no matching entry found", not as
"address already in use".

**B. Splitting `loki-enrich`.** The Docker test covered two things at once: the `loki.enrich` label
join, and `loki.source.api` as an intake. A4 covers the join with `inputs.loki`. The intake is
covered by `loki.source.api`'s unit tests and by the k8s `loki-source-api` test.

**C. `scrape-prom-metrics` became `prometheus-write-paths`.** The Docker test checked that scraped
metrics reach a backend over remote write v1, remote write v2 and OTLP. It stays at k8s level
because the point of those paths is wire compatibility with a real backend. #7225 does cover remote
write v2 metadata as a pipeline test, but against Prometheus's own write handler in the sink, not a
TSDB.

The k8s test runs three scrapes, each under its own `metric-prefix`. Mimir's metadata API is keyed by
metric name with no label matcher, so a shared prefix would make metadata unattributable to a path,
and the one-entry-per-metric check in `QueryMetadata` could fail on conflicting entries. Remote
write v1 asserts metrics only, because it carries metadata out of band. A single scrape fanned out to
three writers would have been more faithful to the Docker test, but scrape fanout is already covered
by `internal/component/prometheus/fanout_test.go`, including both series-ref mapping transitions.

**D. UDP is easier in process.** `loki-gelf` and `loki-syslog` were the awkward cases in the first
version of this plan, because `kubectl port-forward` does not carry UDP. In a pipeline test the
component listens on a real local port and the input dials it. The problem disappears.

**E. RFC3164 has no year.** `loki-syslog` sends both RFC5424 and RFC3164. RFC3164 timestamps omit
the year, so the parser fills it in from the current date. Fixed timestamps in the test file still
work, but assert on the parsed labels rather than on an exact timestamp for the RFC3164 listeners.

**F. File rotation is easier in process, and is the one imperative case.** The Docker test needs a
bind mount because Alloy runs in a container. In a pipeline test the Go test writes to its own temp
directory, so the four rotation strategies need no mount and no container. It still needs C6, because
the test's essence is writing and rotating files on a timeline while Alloy tails them, and the
`CopyTruncate` case needs its timing sleep. This is the test that justifies imperative mode. Keep the
justification that narrow.

**G. Kafka generator.** The compose stack runs `kafka-gen`, which produces to and consumes from
`test_topic`. Without it the exporter reports no `kafka_consumergroup_*` metrics, and the test
asserts on them. Port the generator with the dependency.

**H. Kafka producer location.** Producing from the host through a port-forward fights Kafka
advertised listeners. Run the producer inside the cluster instead, and make it retry: the Docker
`loki-kafka` test flakes in CI with `Tried to send a message to a replica that is not the leader for
some partition`, because the producer connects before leader election settles. Do not carry that
race over.

**I. Tempo.** There is no Tempo dependency in `deps/` yet. B11 needs one, because
`otlp_alloy_integration_metrics_test.go` asserts `otelcol_exporter_send_failed_spans_total`. That
metric only appears when the traces pipeline runs and the deliberately failing exporter fails.

**J. Native histograms need no support in `QueryMetrics`.** The Docker helper simply does `append(metrics, histogramMetrics...)` and checks names
against the same `/series` query, so one combined list works. What the Docker test *does* assert
separately is native histogram **data**: count, sum and buckets. That is now
`mimir.QueryHistograms`, which takes a callback receiving a decoded `HistogramSample` so callers
assert on labels, count, sum or typed buckets. See item 3 of the definition of done for the general
lesson.

**K. Why `unix` stays an integration test.** `prometheus.exporter.unix` reads the host `/proc` and
`/sys`, and the test asserts about 140 metric names. As a pipeline test it would run against the
developer's own operating system, so the list would differ between a Mac laptop and Linux CI. A
container pins the kernel and the OS. Re-validate the list against a kind node before you trust the
result. This step carries the highest flake risk in the plan.

**L. `loki-file-compression` belongs in unit tests, not either suite.** The existing unit matrix at
`file_test.go:455-500` is already 28 cases: `{CRLF, LF}` x `{default, UTF-8, UTF-16, UTF-16LE,
UTF-16BE, UTF-16LE+BOM, UTF-16BE+BOM}` x `{plain, gzipped}`, with fixtures under
`testdata/encoding/`. It asserts decoded content, which the Docker test never did. The Docker test
adds only two things: the `z` format, which no unit test covers at all, and `bz2` or `z` combined
with UTF-16, which the matrix does not cross. Decompression is pure data transformation with no
orchestration or deployment dimension, so extend the table with `.z` and `.bz2` fixtures and delete
the Docker test. That takes 28 cases to roughly 56, running in milliseconds, in place of a
900-entry container test.

**M. Both enrich components already have behavioural unit tests.** `loki/enrich` has `TestEnricher`
and `TestUpdate`. `prometheus/enrich` has `TestEnricher`, `TestValidate` and
`TestEnrichConcurrentUpdate`. So A4 and A6 should assert the pipeline composition, in particular the
mismatched-target case where labels must stay absent, and not re-prove the join logic. A4 does this
with exact label-set matches, so an unmatched host proves no label was added, plus an unmatched total
count.

**N. The push-based receivers are all unit-tested already.** Test function counts:
`loki.source.api` 10, `loki.source.syslog` 9, `loki.source.cloudflare` 7, `loki.source.heroku` 3,
`loki.source.gelf` 1, and `loki/source/aws_firehose` has `component_test.go` and `routes_test.go`.
Those tests already bind the real listener in process. The pipeline conversions must therefore earn
their place on composition, not intake: structured metadata surviving to the write, and labels that
must not be indexed. If a converted test asserts nothing beyond what the unit test does, delete the
Docker test instead of converting it.

**Open decision.** `loki-api` went to k8s instead (#7312), justified by the deployed path: a push
receiver exposed through a Service via `alloy.extraPorts`. With `ForwardPorts` and
`loki.QueryLabelsNotIndexed` in place, `loki-heroku`, `loki-firehose` and the push half of
`loki-gcplog` can follow the same route cheaply. If they do, C3 `inputs.http` loses its only
migration consumers, so decide which route they take before starting A9. `loki-gelf` and
`loki-syslog` are unaffected: they listen on UDP, which `kubectl port-forward` cannot carry, so they
stay in track A either way.

**O. `blackbox` has no behavioural unit test.** `prometheus/exporter/blackbox` only has config tests:
`TestUnmarshalAlloy`, `TestUnmarshalAlloyTargets`, `TestUnmarshalAlloyWithInlineConfig`,
`TestUnmarshalAlloyWithInlineConfigYaml`, `TestUnmarshalAlloyWithInvalidConfig` and
`TestConvertConfig`. Nothing verifies that probing an endpoint produces probe metrics. The Docker
test is the only coverage of that, so A13 is a real conversion rather than a duplicate, and it is
the highest-value row in track A.

**P. Filter log assertions server-side.** `loki.QueryLogs` fetches everything for a test name with
`limit: 1000`, newest first, then filters client-side. Components that emit continuously push early
entries out of that window, so an assertion on something emitted once at startup fails
*intermittently, and more often with a longer timeout*. `QueryLogsPresent` folds each matcher's
labels into the LogQL selector instead, which is what the Docker test did via `LogQuery`. If you add
a log assertion, put the distinguishing label in the selector.

Also use `assert` rather than `require` inside a loop over matchers. With `require`, the first
failure aborts the attempt and the remaining matchers are never evaluated, which hides half the
picture when a test fails.

**Q. Do B10 before B9.** `deps.AlloyOtel` runs `alloy otel --config=...` and requires both
`CollectorConfigPath` and `AlloyConfigPath`, which is exactly B10's shape, and `metamonitor-otel`
already uses it. B9's Docker test runs `entrypoint: ["/bin/otelcol", ...]`, a different binary, with
a collector config that has no alloyengine extension. So B9 additionally needs `AlloyConfigPath`
made optional and a way to select the `/bin/otelcol` entrypoint, or an explicit decision that
`alloy otel` is a faithful enough stand-in.

Both need an `otel-gen` fixture image, which is mechanical: a `Makefile` target mirroring
`prom-gen-image`, two one-line additions in `integration-tests/k8s/runner/main.go` (`maybeBuildImages`
and `loadImages` both hardcode `[]string{cfg.alloyImage, promGenImage}`), and a dep mirroring
`deps/prom_gen.go` with `OTEL_EXPORTER_ENDPOINT` in its manifest. B11 needs it too, so B10 pays for
all three.

## Follow-ups

Found while doing this work. None belong inside a test migration, so each wants its own pull
request.

| Item | Why | Size |
| --- | --- | --- |
| Track dependencies before `Install` in `harness.AddDependency` | `AddDependency` only records a dep after `Install` returns nil, so any error leaks the applied manifests into the shared cluster. `mimir`, `loki`, `mssql`, `redis` and `blackbox-exporter` all still have this. The Alloy, MySQL and Postgres deps work around it by calling `Cleanup()` on each error path, which is the same fix repeated five more times. Fixing the harness removes the need for the workaround everywhere. | small |
| Escape interpolated values in `deps/mimir.go` queries | `QueryMetrics`, `QueryPositive` and `QueryHistograms` build PromQL by string concatenation, so a metric or test name containing a quote produces a malformed query and a confusing parse error. `deps/loki.go` now uses `strconv.Quote`; do the same here. | small |
| Pin the k8s manifest images | The Docker compose pinned digests, the k8s manifests use mutable tags. Pinning is only safe once renovate can maintain it, and `.github/renovate.json5` configures no `kubernetes` manager. So: add that manager with a pattern covering `integration-tests/k8s/deps/manifests/*.yaml`, set `pinDigests: true`, and pin every third-party image together. `prom-gen` is built locally and needs no pin. | medium |
| Rename the `Query*` assertion methods | `mimir.QueryMetrics`, `QueryPositive`, `QueryMetadata`, `QueryHistograms`, `QueryMetricWithLabelsPresent` and `loki.QueryLogs`, `QueryLogsPresent`, `QueryLabelsNotIndexed` all assert and fail the test. The `Query` prefix suggests they return data, which is how the sample-data assertions came to be dropped from the first version of #7215. Rename to `Assert*`, naming what each checks: `AssertMetricNames`, `AssertSamplesPositive`, `AssertMetadata`, `AssertHistograms`, `AssertLogCounts`, `AssertLogsPresent`, `AssertLabelsNotIndexed`. | small |
| Let the assertion helpers take a callback | `QueryHistograms` already takes `func(c *assert.CollectT, sample HistogramSample)` and that shape has proved much better: the caller decides what to check, and the helper only owns the query and the retry. The others bake their checks in, which is why `QueryPositive` cannot express "this metric is legitimately zero" and why `redis` needed two different metric lists. Give the sample, metadata and log helpers the same treatment, keeping the current fixed-assertion forms as thin wrappers where they read well. | medium |

| Fail loudly on unhealthy components in the pipeline harness | `harness.NewAlloy` waits for load but never checks component health, so a component that cannot bind its port shows up as an assertion timeout rather than its real error. See note A. | small |
| Bound the HTTP client in `loki-source-api`'s push helpers | `pushJSON` and `pushProto` use `http.Post` and `http.DefaultClient`, which have no timeout, so a stalled port-forward hangs until the test timeout. `graphql` already uses a 5s client. | small |

The two `Query*` items are related and worth doing together, since renaming and re-shaping the same
methods twice is wasted churn.

## After tracks A and B

The compose file on main runs `kafka`, `kafka-gen`, `loki`, `mimir`, `prom-gen`, `snmp-simulator` and
`tempo`. `redis`, `mysql` and `postgres` have already gone with their tests.

Once tracks A and B are done, these have no consumer left, so delete them and their config
directories: `prom-gen`, whose last user is `prom-enrich` once #7300 merges, `snmp-simulator`,
`kafka` and `kafka-gen`.

`mimir`, `loki` and `tempo` must stay. The tests in tracks C and D still use them.

The `logpullmock` fixture under `tests/loki-cloudflare` goes away with A14, replaced by C5.

## Risk to watch

Track A removes load from CI, because pipeline tests run in process on every pull request and need
no cluster. Track B adds load. The k8s harness installs every dependency once per test package, at
about 20 seconds per dependency. There are 16 k8s test packages on main, and CI runs two shards.

Watch the wall-clock time as track B lands. When it grows too far, promote Mimir and Loki to
install-once cluster fixtures. The k8s README already plans for this. Raising the shard count alone
does not help, because it multiplies the fixed cluster setup and image load cost per shard.
