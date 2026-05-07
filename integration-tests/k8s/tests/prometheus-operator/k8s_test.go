package prometheusoperator

import (
	"testing"

	"github.com/grafana/alloy/integration-tests/k8s/deps"
	"github.com/grafana/alloy/integration-tests/k8s/harness"
)

func TestPrometheusOperator(t *testing.T) {
	ns := deps.NewNamespace(deps.NamespaceOptions{
		Name:   "test-prometheus-operator",
		Labels: map[string]string{"alloy-integration-test": "true"},
	})
	promOp := deps.NewPrometheusOperator(deps.PrometheusOperatorOptions{})
	mimir := deps.NewMimir(deps.MimirOptions{Namespace: ns.Name()})
	alloy := deps.NewAlloy(deps.AlloyOptions{
		Namespace:  ns.Name(),
		Release:    "alloy-prometheus-operator",
		ConfigPath: "./config/config.alloy",
		ValuesPath: "./config/alloy-values.yaml",
	})
	promGen := deps.NewPromGen(deps.PromGenOptions{Namespace: ns.Name()})
	blackbox := deps.NewBlackboxExporter(deps.BlackboxExporterOptions{Namespace: ns.Name()})
	monitoringCRDs := deps.NewCustomWorkloads(deps.CustomWorkloadsOptions{
		Path: "./config/workloads.yaml",
		Vars: map[string]string{"NAMESPACE": ns.Name()},
	})
	kt := harness.Setup(t, harness.Options{
		Dependencies: []harness.Dependency{ns, promOp, promGen, blackbox, monitoringCRDs, mimir, alloy},
	})
	defer kt.Cleanup(t)

	t.Run("ServiceMonitors", func(t *testing.T) {
		// Check that Mimir received metrics from the ServiceMonitor target.
		// All metrics are prefixed with test_servicemonitors_ via relabeling.
		mimir.QueryMetrics(t, "servicemonitor", []string{
			"test_servicemonitors_golang_counter",
			"test_servicemonitors_golang_gauge",
		})
	})

	t.Run("PodMonitors", func(t *testing.T) {
		// Check that Mimir received metrics from the PodMonitor target.
		// All metrics are prefixed with test_podmonitors_ via relabeling.
		mimir.QueryMetrics(t, "podmonitor", []string{
			"test_podmonitors_golang_counter",
			"test_podmonitors_golang_gauge",
		})
	})

	t.Run("Probes", func(t *testing.T) {
		// Check that Mimir received metrics from the Probe target.
		// All metrics are prefixed with test_probes_ via relabeling.
		mimir.QueryMetrics(t, "probe", []string{
			"test_probes_probe_success",
			"test_probes_probe_duration_seconds",
		})
	})

	t.Run("ScrapeConfigs", func(t *testing.T) {
		// Check that Mimir received metrics from the ScrapeConfig target.
		// All metrics are prefixed with test_scrapeconfigs_ via relabeling.
		mimir.QueryMetrics(t, "scrapeconfig", []string{
			"test_scrapeconfigs_golang_counter",
			"test_scrapeconfigs_golang_gauge",
		})
	})
}
