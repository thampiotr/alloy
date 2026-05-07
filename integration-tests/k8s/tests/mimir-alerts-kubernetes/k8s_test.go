package mimiralertskubernetes

import (
	"testing"

	"github.com/grafana/alloy/integration-tests/k8s/deps"
	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/stretchr/testify/require"
)

func TestMimirAlerts(t *testing.T) {
	ns := deps.NewNamespace(deps.NamespaceOptions{
		Name:   "test-mimir-alerts-kubernetes",
		Labels: map[string]string{"alloy-integration-test": "true"},
	})
	promOp := deps.NewPrometheusOperator(deps.PrometheusOperatorOptions{})
	mimir := deps.NewMimir(deps.MimirOptions{Namespace: ns.Name()})
	alloy := deps.NewAlloy(deps.AlloyOptions{
		Namespace:  ns.Name(),
		Release:    "alloy-mimir-alerts-kubernetes",
		ConfigPath: "./config/config.alloy",
		ValuesPath: "./config/alloy-values.yaml",
	})
	extraManifests := deps.NewCustomWorkloads(deps.CustomWorkloadsOptions{
		Path: "./config/workloads.yaml",
		Vars: map[string]string{"NAMESPACE": ns.Name()},
	})
	kt := harness.Setup(t, harness.Options{
		// promOp installs the AlertmanagerConfig CRD that extraManifests
		// applies; mimir waits for its own pod readiness so alloy can
		// reach it as soon as Install returns.
		Dependencies: []harness.Dependency{ns, promOp, extraManifests, mimir, alloy},
	})
	defer kt.Cleanup(t)

	// TODO: move into the alloy dep so tests don't have to know its labels.
	kt.WaitForAllPodsRunning(t, ns.Name(), "app.kubernetes.io/name=alloy")

	t.Run("Initial Config loaded", func(t *testing.T) {
		mimir.CheckConfig(t, "./expected/expected_1.yml")
	})

	t.Run("Deleted Config works", func(t *testing.T) {
		require.NoError(t, harness.Kubectl("delete", "alertmanagerconfig", "alertmgr-config2", "--namespace", ns.Name()))

		// Mimir's config should now omit the deleted Alertmanagerconfig CRD.
		mimir.CheckConfig(t, "./expected/expected_2.yml")
	})
}
