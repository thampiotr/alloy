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
	testNameLabel = "alloy_test_name"
	timeout       = 1 * time.Minute
	retryInterval = 500 * time.Millisecond

	// mimirSelector matches the single Mimir pod created from manifests/mimir.yaml.
	mimirSelector = "app=mimir"
	// mimirHTTPPort is the http_listen_port configured in manifests/mimir.yaml.
	mimirHTTPPort = "9009"
)

//go:embed manifests/mimir.yaml
var mimirManifest string

type metricsResponse struct {
	Status string `json:"status"`
	Data   []struct {
		Name string `json:"__name__"`
	} `json:"data"`
}

// Mimir runs a single-pod Mimir in monolithic mode
// with filesystem storage and inmemory rings.
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

	// Wait for the readiness probe so port-forward and HTTP queries below
	// connect to a usable Service endpoint.
	if err := util.Step("wait for mimir pod ready", func() error {
		return harness.WaitForReady(m.namespace, mimirSelector)
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

// QueryMetrics polls Mimir for series labelled with alloy_test_name=testName
// and asserts every expected metric name is present.
func (m *Mimir) QueryMetrics(t *testing.T, testName string, expectedMetrics []string) {
	t.Helper()
	mimirURL := m.endpoint("/prometheus/api/v1/")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		queryURL, err := url.Parse(mimirURL + "series")
		require.NoError(c, err)
		values := queryURL.Query()
		values.Add("match[]", "{"+testNameLabel+"=\""+testName+"\"}")
		queryURL.RawQuery = values.Encode()
		resp := curl(c, queryURL.String())

		var parsed metricsResponse
		err = json.Unmarshal([]byte(resp), &parsed)
		require.NoError(c, err, "failed to parse mimir response: %s", resp)
		require.Equal(c, "success", parsed.Status, "mimir query failed: %s", resp)

		actualMetrics := make(map[string]struct{}, len(parsed.Data))
		for _, metric := range parsed.Data {
			actualMetrics[metric.Name] = struct{}{}
		}

		var missingMetrics []string
		for _, expectedMetric := range expectedMetrics {
			if _, exists := actualMetrics[expectedMetric]; !exists {
				missingMetrics = append(missingMetrics, expectedMetric)
			}
		}

		require.Emptyf(c, missingMetrics, "missing expected metrics for %s=%s: %v found=%v", testNameLabel, testName, missingMetrics, actualMetrics)
	}, timeout, retryInterval)
}

func (m *Mimir) CheckAlertsConfig(t *testing.T, expectedFile string) {
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

// curlTimeout caps a single HTTP attempt against Mimir. Without it a stalled
// port-forward would block inside the EventuallyWithT callback well past the
// outer retry deadline, masking the failure as a generic timeout.
const curlTimeout = 5 * time.Second

func curl(c *assert.CollectT, targetURL string) string {
	client := http.Client{Timeout: curlTimeout}
	resp, err := client.Get(targetURL)
	require.NoError(c, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(c, err)
	return string(body)
}
