package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Env-var names used to plumb runner-side configuration into test binaries.
// The runner sets these in configureEnvVariables; the harness and deps read
// them. Kept here as the single source of truth so name typos surface as
// build errors rather than silent test misconfiguration.
const (
	ManagedClusterEnv = "ALLOY_TESTS_MANAGED_CLUSTER"
	KubeconfigEnv     = "ALLOY_TESTS_KUBECONFIG"
	KindClusterEnv    = "ALLOY_TESTS_KIND_CLUSTER"
	AlloyImageEnv     = "ALLOY_TESTS_IMAGE"
)

// KindClusterName returns the name of the kind cluster the test runner is
// using, or "" when the runner did not export it. Dependencies that need to
// `kind load docker-image` should call this rather than hardcoding a name.
func KindClusterName() string {
	return os.Getenv(KindClusterEnv)
}

func managedClusterEnabled() bool {
	return os.Getenv(ManagedClusterEnv) == "1"
}

func kubeconfigFromEnv() (string, error) {
	if !managedClusterEnabled() {
		return "", fmt.Errorf("missing %s=1, run tests with make integration-test-k8s or go run ./integration-tests/k8s/runner", ManagedClusterEnv)
	}

	kubeconfig := os.Getenv(KubeconfigEnv)
	if kubeconfig == "" {
		return "", fmt.Errorf("missing %s, run tests with make integration-test-k8s or go run ./integration-tests/k8s/runner", KubeconfigEnv)
	}
	if !filepath.IsAbs(kubeconfig) {
		return "", fmt.Errorf("%s must be an absolute path, got %q", KubeconfigEnv, kubeconfig)
	}
	if _, err := os.Stat(kubeconfig); err != nil {
		return "", fmt.Errorf("%s %q is not accessible: %w", KubeconfigEnv, kubeconfig, err)
	}
	return kubeconfig, nil
}

// CommandEnv returns the process environment with KUBECONFIG forced to the
// managed test kubeconfig (when set). Pass it as cmd.Env when running a long
// lived command directly with exec.Cmd; for one-shot invocations prefer the
// RunCommand* helpers which apply this automatically.
//
// Any pre-existing KUBECONFIG entry inherited from os.Environ() is stripped
// before appending — POSIX permits duplicate keys but tools differ on which
// one wins, so we pin a single deterministic value.
func CommandEnv() []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent)+1)
	for _, kv := range parent {
		if !strings.HasPrefix(kv, "KUBECONFIG=") {
			env = append(env, kv)
		}
	}
	if kubeconfig := os.Getenv(KubeconfigEnv); kubeconfig != "" {
		env = append(env, "KUBECONFIG="+kubeconfig)
	}
	return env
}
