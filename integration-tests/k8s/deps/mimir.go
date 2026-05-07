package deps

import (
	"context"
	_ "embed"
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

	// mimirSelector matches the single Mimir pod created from manifests/mimir.yaml.
	mimirSelector = "app=mimir"
	// mimirHTTPPort is the http_listen_port configured in manifests/mimir.yaml.
	mimirHTTPPort = "9009"
)

//go:embed manifests/mimir.yaml
var mimirManifest string

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

// Mimir runs a single-pod Mimir in monolithic mode (target=all,alertmanager)
// with filesystem storage and inmemory rings. This is intentionally not
// production-shaped; it's the smallest Mimir we can stand up to exercise
// Alloy's remote_write and alertmanager-config push paths in tests.
//
// In-cluster URL: http://mimir:9009 (Service name "mimir", port 9009).
// Tests should point Alloy's `prometheus.remote_write` /
// `mimir.alerts.kubernetes` at that endpoint.
type Mimir struct {
	opts            MimirOptions
	namespace       string
	localPort       string
	stopPortForward func()
	installed       bool
}

type MimirOptions struct {
	// Namespace to install Mimir into. Required.
	Namespace string
}

func NewMimir(opts MimirOptions) *Mimir {
	return &Mimir{opts: opts, namespace: opts.Namespace}
}

func (m *Mimir) Name() string { return "mimir" }

func (m *Mimir) Install(ctx *harness.TestContext) error {
	if m.namespace == "" {
		return fmt.Errorf("mimir namespace is required")
	}

	if err := util.Step("apply mimir manifest", func() error {
		return harness.RunCommandStdin(mimirManifest,
			"kubectl", "apply", "--namespace", m.namespace, "-f", "-",
		)
	}); err != nil {
		return err
	}
	m.installed = true
	ctx.AddDiagnosticHook("mimir logs", m.diagnosticsHook())

	if err := util.Step("wait for mimir pod ready", func() error {
		// `kubectl wait --for=condition=ready` blocks until the readiness
		// probe (HTTP /ready) passes. Mimir's /ready returns 200 only once
		// all in-process targets (distributor, ingester, alertmanager, ...)
		// are ready, so this also guarantees the API is usable before
		// port-forward connects to a Service endpoint.
		return harness.RunCommand("kubectl",
			"--namespace", m.namespace,
			"wait", "--for=condition=ready", "pod",
			"-l", mimirSelector,
			"--timeout=5m",
		)
	}); err != nil {
		return err
	}

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
		return
	}
	_ = harness.RunCommandStdin(mimirManifest,
		"kubectl", "delete", "--namespace", m.namespace, "-f", "-",
		"--ignore-not-found=true", "--wait=true", "--timeout=10m",
	)
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
		// Single Mimir pod in monolithic mode -> one selector covers
		// distributor, ingester and alertmanager logs.
		return harness.RunDiagnosticCommands(c, [][]string{
			{"kubectl", "--namespace", namespace, "logs", "-l", mimirSelector, "--all-containers=true", "--tail", "500"},
			{"kubectl", "--namespace", namespace, "describe", "pod", "-l", mimirSelector},
		})
	}
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
		"service/mimir",
		localPort+":"+mimirHTTPPort,
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
