package deps

import (
	_ "embed"
	"fmt"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

//go:embed manifests/prom-gen.yaml
var promGenManifest string

const (
	promGenImage = "prom-gen:latest"
	// TODO: currently we use the one from docker integration tests. Consider
	// moving it to the k8s integration tests in the future.
	promGenDockerfile = "integration-tests/docker/configs/prom-gen/Dockerfile"
	promGenSelector   = "app=prom-gen"
)

// PromGen is a small Go HTTP server that emits Prometheus metrics. It's used
// as a scrape target by tests that exercise prometheus operator components.
// Owns its docker image, Deployment + Service manifest, and the readiness
// wait, so a test only has to add one entry to its dependency list.
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

func (p *PromGen) Install(ctx *harness.TestContext) error {
	if p.opts.Namespace == "" {
		return fmt.Errorf("prom-gen namespace is required")
	}
	if err := ensureKindImage(promGenImage, true, promGenDockerfile, "."); err != nil {
		return err
	}
	if err := util.Step("apply prom-gen manifest", func() error {
		return harness.RunCommandStdin(promGenManifest,
			"kubectl", "apply", "--namespace", p.opts.Namespace, "-f", "-",
		)
	}); err != nil {
		return err
	}
	p.installed = true
	return util.Step("wait for prom-gen pod running", func() error {
		return ctx.AwaitAllPodsRunning(p.opts.Namespace, promGenSelector)
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
