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
	// pod selector if the previous attempt failed (typically because no
	// pods existed yet between apply and the controller creating them).
	readyAttemptTimeout = "15s"
	// readyPollInterval is the gap between retries. Short enough that a
	// freshly-applied Deployment is picked up promptly.
	readyPollInterval = 1 * time.Second
)

// Kubectl runs kubectl with the managed test kubeconfig. Returns an error if
// no args are given so accidental empty invocations fail loudly.
func Kubectl(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("kubectl requires at least one argument")
	}
	return RunCommand("kubectl", args...)
}

// WaitForReady blocks until at least one pod matching selector in namespace
// reports condition=Ready, or readyTimeout elapses. It loops on
// `kubectl wait --for=condition=ready pod -l <selector>` because that command
// fails immediately with "no matching resources found" when no pods exist
// yet (a race against the Deployment/DaemonSet/StatefulSet controller right
// after apply). Each attempt blocks for at most readyAttemptTimeout, then we
// retry until the readyTimeout budget is spent.
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
