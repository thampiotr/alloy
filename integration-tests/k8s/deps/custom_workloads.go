package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

type CustomWorkloadsOptions struct {
	// Path is the path to a YAML manifest applied with `kubectl apply -f` on
	// Install and removed with `kubectl delete -f` on Cleanup.
	Path string
	// Vars is an optional map of placeholders substituted in the manifest
	// before it is applied or deleted. Each `${KEY}` in the file is replaced
	// with vars[KEY]. Unresolved placeholders fail Install loudly to catch
	// typos and missing values early. See util.SubstituteVars.
	Vars map[string]string
}

type CustomWorkloads struct {
	opts    CustomWorkloadsOptions
	absPath string
}

func NewCustomWorkloads(opts CustomWorkloadsOptions) *CustomWorkloads {
	return &CustomWorkloads{opts: opts}
}

func (w *CustomWorkloads) Name() string {
	return "custom-workloads"
}

func (w *CustomWorkloads) Install(_ *harness.TestContext) error {
	if w.opts.Path == "" {
		return fmt.Errorf("custom workloads path is required")
	}
	absPath, err := filepath.Abs(w.opts.Path)
	if err != nil {
		return fmt.Errorf("resolve custom workloads path: %w", err)
	}
	w.absPath = absPath

	manifest, err := w.renderManifest()
	if err != nil {
		return err
	}
	return runKubectlWithManifest(manifest, "apply", "-f", "-")
}

func (w *CustomWorkloads) Cleanup() {
	if w.absPath == "" {
		return
	}
	manifest, err := w.renderManifest()
	if err != nil {
		// Render failures during Cleanup are unexpected (Install would have
		// caught them), but don't escalate beyond a log line: Cleanup must
		// always be best-effort.
		fmt.Fprintf(os.Stderr, "[k8s-itest] custom-workloads cleanup render failed: %v\n", err)
		return
	}
	_ = runKubectlWithManifest(manifest, "delete", "-f", "-",
		"--ignore-not-found=true", "--wait=true", "--timeout=10m",
	)
}

func (w *CustomWorkloads) renderManifest() (string, error) {
	raw, err := os.ReadFile(w.absPath)
	if err != nil {
		return "", fmt.Errorf("read workloads %s: %w", w.absPath, err)
	}
	rendered, err := util.SubstituteVars(string(raw), w.opts.Vars)
	if err != nil {
		return "", fmt.Errorf("workloads %s: %w", w.absPath, err)
	}
	return rendered, nil
}

func runKubectlWithManifest(manifest string, args ...string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv()
	return cmd.Run()
}
