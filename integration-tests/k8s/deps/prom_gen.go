package deps

import (
	_ "embed"
	"fmt"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

//go:embed manifests/prom-gen.yaml
var promGenManifest string

// promGenSelector matches the single prom-gen pod created from
// manifests/prom-gen.yaml. The image itself ("prom-gen:latest") is built
// and kind-loaded by the runner — see Makefile target `prom-gen-image` and
// runner.maybeBuildImages — so this dep only has to apply the manifest.
const promGenSelector = "app=prom-gen"

// PromGen is a small Go HTTP server that emits Prometheus metrics. It's used
// as a scrape target by tests that exercise prometheus operator components.
type PromGen struct {
	opts      PromGenOptions
	installed bool
}

type PromGenOptions struct {
	// Namespace into which prom-gen is deployed. Required.
	Namespace string
}

func NewPromGen(opts PromGenOptions) *PromGen {
	return &PromGen{opts: opts}
}

func (p *PromGen) Name() string { return "prom-gen" }

func (p *PromGen) Install(_ *harness.TestContext) error {
	if p.opts.Namespace == "" {
		return fmt.Errorf("prom-gen namespace is required")
	}
	if err := util.Step("apply prom-gen manifest", func() error {
		return harness.RunCommandStdin(promGenManifest,
			"kubectl", "apply", "--namespace", p.opts.Namespace, "-f", "-",
		)
	}); err != nil {
		return err
	}
	p.installed = true
	return util.Step("wait for prom-gen pod ready", func() error {
		return harness.WaitForReady(p.opts.Namespace, promGenSelector)
	})
}

func (p *PromGen) Cleanup() {
	if !p.installed {
		return
	}
	_ = harness.RunCommandStdin(promGenManifest,
		"kubectl", "delete", "--namespace", p.opts.Namespace, "-f", "-",
		"--ignore-not-found=true", "--wait=true", "--timeout=10m",
	)
}
