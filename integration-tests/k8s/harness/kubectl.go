package harness

import (
	"fmt"
	"time"
)

const (
	// readyTimeout bounds WaitForReady end-to-end. 5m comfortably covers
	// every current dep on a fresh kind cluster (helm rollouts, image
	// pulls, alertmanager bootstrap).
	readyTimeout = 5 * time.Minute
	// readyAttemptTimeout caps each kubectl wait call so we re-check the
	// pod selector if the previous attempt timed out.
	readyAttemptTimeout = "15s"
	// readyPollInterval is the gap between retries.
	readyPollInterval = 1 * time.Second
)

// ApplyManifest applies a YAML manifest from memory using `kubectl apply -f -`.
// When namespace is non-empty, --namespace is included; pass "" for manifests
// that are cluster-scoped or already declare metadata.namespace.
func ApplyManifest(namespace, manifest string) error {
	args := []string{"apply"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	args = append(args, "-f", "-")
	return RunCommandStdin(manifest, "kubectl", args...)
}

// DeleteManifest deletes the resources described by a YAML manifest. Always
// passes --ignore-not-found, --wait and --timeout=10m so cleanup paths are
// idempotent and don't leak resources between tests. namespace works the
// same way as in ApplyManifest.
func DeleteManifest(namespace, manifest string) error {
	args := []string{"delete"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	args = append(args, "-f", "-",
		"--ignore-not-found=true", "--wait=true", "--timeout=10m",
	)
	return RunCommandStdin(manifest, "kubectl", args...)
}

// WaitForReady blocks until at least one pod matching selector in namespace
// reports condition=Ready, or readyTimeout elapses.
//
// Call this from a Dependency's Install before returning, so callers can
// rely on "Install has returned" meaning "the dep is usable".
func WaitForReady(namespace, selector string) error {
	deadline := time.Now().Add(readyTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		err := RunCommand("kubectl",
			"--namespace", namespace,
			"wait", "--for=condition=ready", "pod",
			"-l", selector,
			"--timeout="+readyAttemptTimeout,
		)
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(readyPollInterval)
	}
	return fmt.Errorf("timed out after %s waiting for pods ready namespace=%s selector=%s: %w", readyTimeout, namespace, selector, lastErr)
}
