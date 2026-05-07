// Package harness wires Go tests in integration-tests/k8s/tests/... to the
// runner-managed kind cluster. Tests call Setup with a list of Dependencies
// (deps.Namespace, deps.Mimir, ...); Setup installs them in order, returns a
// *TestContext, and tests defer Cleanup which tears them down in reverse.
package harness

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/alloy/integration-tests/k8s/util"
)

// Dependency is implemented by every test fixture (deps.Namespace,
// deps.Mimir, deps.Alloy, ...). Install must block until the dep is usable
// (typically by calling WaitForReady) so callers can rely on "Install
// returned" meaning "dep is ready". Cleanup is best-effort.
type Dependency interface {
	Name() string
	Install(*TestContext) error
	Cleanup()
}

type Options struct {
	// Dependencies is a list of dependencies to install in order. They are
	// cleaned up in reverse order.
	Dependencies []Dependency
}

// TestContext is the runtime context for a test. It tracks installed
// dependencies and registered diagnostic hooks. Namespace ownership lives
// in dependencies (e.g. deps.Namespace), not here.
type TestContext struct {
	name            string
	pkgPath         string
	dependencies    []Dependency
	diagnosticHooks []diagnosticHook
}

func Setup(t *testing.T, opts Options) *TestContext {
	// IMPORTANT: capture the caller's file path on the very first line so the
	// runtime.Caller frame depth (1) always points at the test that invoked
	// Setup. Any helper introduced before this line would silently shift the
	// frame and the failure-diagnostics repro hint would lose accuracy.
	_, callerFile, _, _ := runtime.Caller(1)

	t.Helper()
	shardCheck(t, t.Name())
	if !managedClusterEnabled() {
		t.Skip("requires managed k8s test runner; use make integration-test-k8s")
	}

	if _, err := kubeconfigFromEnv(); err != nil {
		t.Fatalf("%v", err)
	}

	ctx := &TestContext{
		name:    t.Name(),
		pkgPath: derivePkgPath(callerFile),
	}

	for _, dep := range opts.Dependencies {
		err := util.Step("install dep "+dep.Name(), func() error { return dep.Install(ctx) })
		if err != nil {
			t.Fatalf("install dependency %q: %v", dep.Name(), err)
		}
		ctx.dependencies = append(ctx.dependencies, dep)
	}

	return ctx
}

// derivePkgPath returns a repo-rooted package path (e.g.
// "integration-tests/k8s/tests/mimir-alerts-kubernetes") suitable for the
// failure-diagnostics repro hint. We trim everything before the
// "integration-tests/" boundary because all k8s integration tests live under
// that directory; if the framework ever moves elsewhere this needs an update.
func derivePkgPath(callerFile string) string {
	if callerFile == "" {
		return ""
	}
	const marker = "integration-tests/"
	if idx := strings.Index(callerFile, marker); idx >= 0 {
		return filepath.Dir(callerFile[idx:])
	}
	return filepath.Dir(callerFile)
}

func (ctx *TestContext) Cleanup(t *testing.T) {
	t.Helper()

	if t.Failed() {
		collectFailureDiagnostics(ctx)
	}
	for i := len(ctx.dependencies) - 1; i >= 0; i-- {
		dep := ctx.dependencies[i]
		_ = util.Step("cleanup dep "+dep.Name(), func() error {
			dep.Cleanup()
			return nil
		})
	}
}

func (ctx *TestContext) AddDiagnosticHook(name string, fn func(context.Context) error) {
	ctx.diagnosticHooks = append(ctx.diagnosticHooks, diagnosticHook{name: name, fn: fn})
}
