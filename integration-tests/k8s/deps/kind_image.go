package deps

import (
	"fmt"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

// ensureKindImage pulls the named docker image (if not already present
// locally) and loads it into the kind cluster managed by the runner.
//
// The runner pre-builds and pre-loads images that are produced from this
// repo (alloy, prom-gen). This helper exists for dependencies that pull a
// pinned image from a public registry — e.g. prom/blackbox-exporter — so
// each test can declare the image it needs without the runner having to
// know about every transitive dep.
//
// The kind cluster name is read from harness.KindClusterName(); the runner
// exports it via the ALLOY_TESTS_KIND_CLUSTER env var.
func ensureKindImage(image string) error {
	if image == "" {
		return fmt.Errorf("image is required")
	}
	cluster := harness.KindClusterName()
	if cluster == "" {
		return fmt.Errorf("kind cluster name not set; ensure the test runner exported ALLOY_TESTS_KIND_CLUSTER")
	}

	if err := ensureLocalImage(image); err != nil {
		return err
	}
	return util.Step(fmt.Sprintf("kind load %s", image), func() error {
		return harness.RunCommand("kind", "load", "docker-image", image, "--name", cluster)
	})
}

// ensureLocalImage pulls the image when it's not already in the local
// daemon. The presence check keeps repeated local iterations quiet and
// also short-circuits in CI where the runner pre-loaded the image.
func ensureLocalImage(image string) error {
	if err := harness.RunCommandQuiet("docker", "image", "inspect", image); err == nil {
		return nil
	}
	return util.Step(fmt.Sprintf("docker pull %s", image), func() error {
		return harness.RunCommand("docker", "pull", image)
	})
}
