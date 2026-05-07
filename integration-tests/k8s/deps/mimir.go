package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	intTestLabel  = "alloy_int_test"
	timeout       = 5 * time.Minute
	retryInterval = 500 * time.Millisecond
)

type MetricsResponse struct {
	Status string `json:"status"`
	Data   []struct {
		Name string `json:"__name__"`
	} `json:"data"`
}

type MetadataResponse struct {
	Status string                     `json:"status"`
	Data   map[string][]MetadataEntry `json:"data"`
}

type MetadataEntry struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit"`
}

type ExpectedMetadata struct {
	Type string
	Help string
}

// mimirHelmRelease is the helm release name used by Mimir.Install. Kept as a
// constant so Cleanup can uninstall the release without depending on Install
// having succeeded fully.
const mimirHelmRelease = "mimir"

type Mimir struct {
	namespace       string
	localPort       string
	stopPortForward func()
	installed       bool
}

type MimirOptions struct {
	Namespace string
}

func NewMimir(opts MimirOptions) *Mimir {
	return &Mimir{namespace: opts.Namespace}
}

func (m *Mimir) Name() string {
	return "mimir"
}

func (m *Mimir) Install(ctx *harness.TestContext) error {
	if m.namespace == "" {
		return fmt.Errorf("mimir namespace is required")
	}

	if err := installMimir(m.namespace); err != nil {
		return err
	}
	m.installed = true
	ctx.AddDiagnosticHook("mimir logs", m.diagnosticsHook())

	localPort, stop, err := startPortForwardWithRetries(m.namespace, 5)
	if err != nil {
		return err
	}
	m.localPort = localPort
	m.stopPortForward = stop
	return nil
}

func (m *Mimir) Cleanup() {
	if m.stopPortForward != nil {
		m.stopPortForward()
	}
	if !m.installed || m.namespace == "" {
		// Install never reached helm install; nothing to uninstall.
		return
	}
	_ = util.Step("uninstall mimir helm release", func() error {
		return harness.RunCommand(
			"helm", "uninstall", mimirHelmRelease,
			"--namespace", m.namespace,
			"--ignore-not-found",
			"--wait",
		)
	})
}

func (m *Mimir) QueryMetrics(t *testing.T, alloyIntTest string, expectedMetrics []string) {
	t.Helper()
	mimirURL := m.endpoint("/prometheus/api/v1/")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		queryURL, err := url.Parse(mimirURL + "series")
		require.NoError(c, err)
		values := queryURL.Query()
		values.Add("match[]", "{"+intTestLabel+"=\""+alloyIntTest+"\"}")
		queryURL.RawQuery = values.Encode()
		resp := curl(c, queryURL.String())

		var metricsResponse MetricsResponse
		err = json.Unmarshal([]byte(resp), &metricsResponse)
		require.NoError(c, err, "failed to parse mimir response: %s", resp)
		require.Equal(c, "success", metricsResponse.Status, "mimir query failed: %s", resp)

		actualMetrics := make(map[string]struct{}, len(metricsResponse.Data))
		for _, metric := range metricsResponse.Data {
			actualMetrics[metric.Name] = struct{}{}
		}

		var missingMetrics []string
		for _, expectedMetric := range expectedMetrics {
			if _, exists := actualMetrics[expectedMetric]; !exists {
				missingMetrics = append(missingMetrics, expectedMetric)
			}
		}

		require.Emptyf(c, missingMetrics, "missing expected metrics for %s=%s: %v found=%v", intTestLabel, alloyIntTest, missingMetrics, actualMetrics)
	}, timeout, retryInterval)
}

func (m *Mimir) QueryMetadata(t *testing.T, expectedMetadata map[string]ExpectedMetadata) {
	t.Helper()
	mimirURL := m.endpoint("/prometheus/api/v1/")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		resp := curl(c, mimirURL+"metadata")
		var metadataResponse MetadataResponse
		err := json.Unmarshal([]byte(resp), &metadataResponse)
		require.NoError(c, err, "failed to parse metadata response: %s", resp)
		require.Equal(c, "success", metadataResponse.Status, "mimir metadata query failed: %s", resp)

		var missingMetrics []string
		var mismatchedMetrics []string
		for metricName, expected := range expectedMetadata {
			entries, exists := metadataResponse.Data[metricName]
			if !exists || len(entries) == 0 {
				missingMetrics = append(missingMetrics, metricName)
				continue
			}
			entry := entries[0]
			if expected.Type != "" && entry.Type != expected.Type {
				mismatchedMetrics = append(mismatchedMetrics, metricName+": expected type="+expected.Type+", got="+entry.Type)
			}
			if expected.Help != "" && entry.Help != expected.Help {
				mismatchedMetrics = append(mismatchedMetrics, metricName+": expected help="+expected.Help+", got="+entry.Help)
			}
		}

		require.Emptyf(c, missingMetrics, "missing expected metadata for metrics: %v", missingMetrics)
		require.Emptyf(c, mismatchedMetrics, "mismatched metadata: %v", mismatchedMetrics)
	}, timeout, retryInterval)
}

func (m *Mimir) CheckConfig(t *testing.T, expectedFile string) {
	t.Helper()
	expectedMimirConfigBytes, err := os.ReadFile(expectedFile)
	require.NoError(t, err)
	expectedMimirConfig := string(expectedMimirConfigBytes)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		actualMimirConfig := curl(c, m.endpoint("/api/v1/alerts"))
		require.Equal(c, expectedMimirConfig, actualMimirConfig)
	}, timeout, retryInterval)
}

func (m *Mimir) endpoint(path string) string {
	return "http://localhost:" + m.localPort + path
}

func (m *Mimir) diagnosticsHook() func(context.Context) error {
	namespace := m.namespace
	return func(c context.Context) error {
		// The harness already wraps each hook in a per-hook timeout (see
		// harness.collectFailureDiagnostics), so we just forward c.
		return harness.RunDiagnosticCommands(c, [][]string{
			{"kubectl", "--namespace", namespace, "logs", "-l", "app.kubernetes.io/component=distributor", "--all-containers=true", "--tail", "200"},
			{"kubectl", "--namespace", namespace, "logs", "-l", "app.kubernetes.io/component=alertmanager", "--all-containers=true", "--tail", "200"},
		})
	}
}

func installMimir(namespace string) error {
	if err := util.Step("helm repo add grafana", func() error {
		return harness.RunCommand("helm", "repo", "add", "grafana", "https://grafana.github.io/helm-charts")
	}); err != nil {
		return err
	}
	if err := util.Step("helm repo update", func() error {
		return harness.RunCommand("helm", "repo", "update")
	}); err != nil {
		return err
	}
	return util.Step("install mimir", func() error {
		return harness.RunCommand(
			"helm",
			"upgrade",
			"--install",
			"mimir",
			"grafana/mimir-distributed",
			"--version", "5.8.0",
			"--namespace", namespace,
			"--wait",
			// TODO: we should probably use values.yaml instead of --set and keep
			// mimir-values.yaml in the test config/ directory like we do with alloy.
			"--set", "ingester.replicas=1",
			"--set", "querier.replicas=1",
			"--set", "query_scheduler.enabled=false",
			"--set", "store_gateway.enabled=false",
			"--set", "compactor.enabled=false",
			"--set", "admin_api.enabled=false",
			"--set", "gateway.enabled=false",
		)
	})
}

func startPortForwardWithRetries(namespace string, attempts int) (string, func(), error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		localPort, err := pickFreeLocalPort()
		if err != nil {
			lastErr = err
			continue
		}
		stop, err := startPortForward(namespace, localPort)
		if err == nil {
			return localPort, stop, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unable to allocate local port for port-forward")
	}
	return "", nil, fmt.Errorf("failed to start mimir port-forward after %d attempts: %w", attempts, lastErr)
}

func startPortForward(namespace, localPort string) (func(), error) {
	cmd := exec.CommandContext(
		context.Background(),
		"kubectl",
		"port-forward",
		"--namespace", namespace,
		"service/mimir-nginx",
		localPort+":80",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = harness.CommandEnv()
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		return nil, fmt.Errorf("port-forward exited early: %w", err)
	case <-time.After(500 * time.Millisecond):
	}

	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
		}
	}, nil
}

func pickFreeLocalPort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return "", err
	}
	return port, nil
}

func curl(c *assert.CollectT, targetURL string) string {
	resp, err := http.Get(targetURL)
	require.NoError(c, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(c, err)
	return string(body)
}
