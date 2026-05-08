package deps

import (
	"fmt"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
)

// defaultPrometheusOperatorVersion is the prometheus-operator release used
// when PrometheusOperatorOptions.Version is empty. Pin it to a known-good
// version so tests are reproducible across runs.
const defaultPrometheusOperatorVersion = "v0.81.0"

// CRDs we actually consume in tests; we wait for these to register so that
// later deps applying CRs (ServiceMonitor, PodMonitor, ...) don't race the
// apiserver and fail with "no matches for kind".
var establishedCRDs = []string{
	"crd/servicemonitors.monitoring.coreos.com",
	"crd/podmonitors.monitoring.coreos.com",
	"crd/probes.monitoring.coreos.com",
	"crd/scrapeconfigs.monitoring.coreos.com",
	"crd/alertmanagerconfigs.monitoring.coreos.com",
}

// PrometheusOperator installs the upstream prometheus-operator bundle, which
// provides the CRDs (ServiceMonitor, PodMonitor, Probe, ScrapeConfig,
// AlertmanagerConfig, etc.) Alloy components consume in tests.
//
// The operator deployment itself is also installed as part of the bundle but
// is not required by Alloy components — they read the CRs directly via the
// Kubernetes API. We install it anyway because the upstream bundle ships
// everything together and splitting it has no benefit.
type PrometheusOperator struct {
	opts PrometheusOperatorOptions
}

type PrometheusOperatorOptions struct {
	// Version is the upstream prometheus-operator release tag (e.g.
	// "v0.81.0"). When empty, defaultPrometheusOperatorVersion is used.
	Version string
}

func NewPrometheusOperator(opts PrometheusOperatorOptions) *PrometheusOperator {
	return &PrometheusOperator{opts: opts}
}

func (p *PrometheusOperator) Name() string { return "prometheus-operator" }

func (p *PrometheusOperator) Install(_ *harness.TestContext) error {
	v := p.opts.Version
	if v == "" {
		v = defaultPrometheusOperatorVersion
	}
	url := fmt.Sprintf(
		"https://github.com/prometheus-operator/prometheus-operator/releases/download/%s/bundle.yaml",
		v,
	)
	if err := harness.RunCommand("kubectl", "apply",
		"--server-side", "--validate=false", "-f", url,
	); err != nil {
		return fmt.Errorf("apply prometheus-operator bundle %s: %w", v, err)
	}
	args := append([]string{"wait", "--for=condition=established", "--timeout=2m"}, establishedCRDs...)
	if err := harness.RunCommand("kubectl", args...); err != nil {
		return fmt.Errorf("wait for prometheus-operator CRDs to be established: %w", err)
	}
	return nil
}

// Cleanup is intentionally a no-op: CRDs are cluster-scoped and harmless to
// leave around, and the kind cluster lifecycle (or a subsequent test reusing
// the cluster) handles tear-down. Leaving the bundle in place also makes
// `kubectl apply` idempotent across multiple tests in the same run.
func (p *PrometheusOperator) Cleanup() {}
