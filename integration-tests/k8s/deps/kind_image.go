package deps

import (
	"fmt"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

// ensureKindImage makes sure the named docker image exists locally and is
// loaded into the kind cluster managed by the runner.
//
// When build is true, the image is built with `docker build` if it's missing
// from the local daemon; when build is false, it is pulled with `docker pull`
// instead. In both cases, if the image is already present locally, the
// build/pull step is skipped to keep dev iterations fast and quiet.
//
// The kind cluster name is read from harness.KindClusterName(); the runner
// exports it via the ALLOY_TESTS_KIND_CLUSTER env var.
func ensureKindImage(image string, build bool, dockerfile, buildContext string) error {
	if image == "" {
		return fmt.Errorf("image is required")
	}
	cluster := harness.KindClusterName()
	if cluster == "" {
		return fmt.Errorf("kind cluster name not set; ensure the test runner exported ALLOY_TESTS_KIND_CLUSTER")
	}

	if err := ensureLocalImage(image, build, dockerfile, buildContext); err != nil {
		return err
	}
	return util.Step(fmt.Sprintf("kind load %s", image), func() error {
		return harness.RunCommand("kind", "load", "docker-image", image, "--name", cluster)
	})
}

// ensureLocalImage skips work when the image is already present in the local
// docker daemon, which keeps repeated local iterations quiet.
func ensureLocalImage(image string, build bool, dockerfile, buildContext string) error {
	if err := harness.RunCommandQuiet("docker", "image", "inspect", image); err == nil {
		// Already present; nothing to do.
		return nil
	}
	if build {
		if dockerfile == "" || buildContext == "" {
			return fmt.Errorf("build=true requires dockerfile and buildContext for %q", image)
		}
		return util.Step(fmt.Sprintf("docker build %s", image), func() error {
			return harness.RunCommand("docker", "build", "-t", image, "-f", dockerfile, buildContext)
		})
	}
	return util.Step(fmt.Sprintf("docker pull %s", image), func() error {
		return harness.RunCommand("docker", "pull", image)
	})
}
