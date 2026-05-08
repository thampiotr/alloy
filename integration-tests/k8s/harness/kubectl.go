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

// ApplyManifest pipes manifest into `kubectl apply -f -`. Pass an empty
// namespace for cluster-scoped manifests or those declaring their own.
func ApplyManifest(namespace, manifest string) error {
	args := []string{"apply"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	args = append(args, "-f", "-")
	return RunCommandStdin(manifest, "kubectl", args...)
}

// DeleteManifest mirrors ApplyManifest for `kubectl delete`. Always passes
// --ignore-not-found, --wait and --timeout=10m so cleanup is idempotent.
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

// WaitForReady blocks until every pod matching selector in namespace reports
// condition=Ready, or readyTimeout elapses. The retry loop swallows the
// transient "no matching resources found" error so callers can call this
// straight after a kubectl apply without racing pod creation.
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
