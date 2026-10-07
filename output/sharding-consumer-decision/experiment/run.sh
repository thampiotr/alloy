#!/bin/sh
# Reconstruct the original isolated experiment without adding an Alloy module.
set -eu
experiment_dir=$(CDPATH= cd -- "$(dirname -- "$0")/testdata" && pwd)
experiment_tmp=$(mktemp -d "${TMPDIR:-/tmp}/alloy-sharding-experiment.XXXXXX")
trap 'rm -rf "$experiment_tmp"' EXIT HUP INT TERM
mkdir -p "$experiment_tmp/prometheus-common/model"
cp "$experiment_dir/consumer_sharding.go" "$experiment_dir/model.go" "$experiment_dir/scheduling_test.go" "$experiment_tmp/"
cp "$experiment_dir/go.mod.fixture" "$experiment_tmp/go.mod"
cp "$experiment_dir/prometheus-common/go.mod.fixture" "$experiment_tmp/prometheus-common/go.mod"
cp "$experiment_dir/prometheus-common/model/model.go" "$experiment_tmp/prometheus-common/model/"
cd "$experiment_tmp"
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -race -v -count=1 -timeout=30s ./...
