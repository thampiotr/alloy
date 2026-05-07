package harness

import (
	"context"
	"fmt"
	"os"
	"time"
)

const diagTimeout = 20 * time.Second

type diagnosticHook struct {
	name string
	fn   func(context.Context) error
}

func collectFailureDiagnostics(ctx *TestContext) {
	fmt.Printf("[k8s-itest] collecting failure diagnostics for test %q\n", ctx.name)
	for _, hook := range ctx.diagnosticHooks {
		hookCtx, cancel := context.WithTimeout(context.Background(), diagTimeout)
		start := time.Now()
		err := hook.fn(hookCtx)
		cancel()
		if err != nil {
			fmt.Printf("[k8s-itest] diagnostics hook failed name=%q time=%s err=%v\n", hook.name, time.Since(start).Round(time.Millisecond), err)
			continue
		}
		fmt.Printf("[k8s-itest] diagnostics hook done name=%q time=%s\n", hook.name, time.Since(start).Round(time.Millisecond))
	}
	if ctx.pkgPath != "" {
		fmt.Printf("[k8s-itest] repro: make integration-test-k8s RUN_ARGS='--package ./%s --run %s'\n", ctx.pkgPath, ctx.name)
	} else {
		fmt.Printf("[k8s-itest] repro: make integration-test-k8s RUN_ARGS='--run %s'\n", ctx.name)
	}
	fmt.Printf("[k8s-itest] kubeconfig: %s\n", os.Getenv(kubeconfigEnv))
}
