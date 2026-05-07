# Kubernetes Integration Tests

`integration-tests/k8s/runner` is the canonical entrypoint. It always uses a
runner-managed kind cluster and kubeconfig (never your default kube context),
then executes `go test` for `integration-tests/k8s/tests/...`.

## CI / one-shot run

```sh
make integration-test-k8s
```

Useful options (forwarded with `RUN_ARGS`):

```sh
make integration-test-k8s RUN_ARGS='--reuse-cluster'
make integration-test-k8s RUN_ARGS='--skip-alloy-build'
# Force a clean slate before the run. Pair with --reuse-cluster to also keep
# the freshly-created cluster for the next iteration.
make integration-test-k8s RUN_ARGS='--delete-cluster --reuse-cluster'
# Split test packages across 2 shards and run shard index 0.
make integration-test-k8s RUN_ARGS='--shard 0/2'
make integration-test-k8s RUN_ARGS='--package ./integration-tests/k8s/tests/prometheus-operator'
```

## Local dev (interactive menu)

```sh
make integration-test-k8s-local-dev
```

Opens a small TUI to pick the common run options before tests start:

- multi-select: reuse kind cluster (default-on), skip Alloy image build (default-on), delete kind cluster before run (default-off; combine with reuse to start fresh and keep)
- single-select: run all tests (default), filter by shard (CI-style `i/n`), or pick test packages
- conditional: shard input or multi-select of packages

Use arrows to navigate, space to toggle, enter to confirm.

Per-test Alloy chart options (controller type, replicas, stability level, etc.)
are set via a helm values file in the test's `config/alloy-values.yaml` and
passed to `deps.NewAlloy(deps.AlloyOptions{ValuesPath: ...})`.

If reuse mode leaves a broken cluster behind:

```sh
kind delete cluster --name alloy-k8s-integration
```
