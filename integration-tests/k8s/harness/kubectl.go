package harness

import "fmt"

// readyTimeout bounds WaitForReady. 5m comfortably covers every current dep
// on a fresh kind cluster (helm rollouts, image pulls, alertmanager bootstrap).
const readyTimeout = "5m"

// Kubectl runs kubectl with the managed test kubeconfig. Returns an error if
// no args are given so accidental empty invocations fail loudly.
func Kubectl(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("kubectl requires at least one argument")
	}
	return RunCommand("kubectl", args...)
}

// WaitForReady blocks until at least one pod matching selector in namespace
// reports condition=Ready, or readyTimeout elapses. It wraps
// `kubectl wait --for=condition=ready pod -l <selector> --timeout=<dur>` so
// every dep waits the same way and tests don't need to know selectors.
//
// Call this from a Dependency's Install before returning, so callers can
// rely on "Install has returned" meaning "the dep is usable".
func WaitForReady(namespace, selector string) error {
	return RunCommand("kubectl",
		"--namespace", namespace,
		"wait", "--for=condition=ready", "pod",
		"-l", selector,
		"--timeout="+readyTimeout,
	)
}
