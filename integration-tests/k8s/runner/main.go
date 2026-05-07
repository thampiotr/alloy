package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/grafana/alloy/integration-tests/k8s/harness"
	"github.com/grafana/alloy/integration-tests/k8s/util"
)

const (
	clusterName        = "alloy-k8s-integration"
	kubeconfigEnv      = "ALLOY_TESTS_KUBECONFIG"
	managedEnv         = "ALLOY_TESTS_MANAGED_CLUSTER"
	alloyImageEnv      = "ALLOY_TESTS_IMAGE"
	kindClusterNameEnv = "ALLOY_TESTS_KIND_CLUSTER"
)

// defaultTestPackages is the fallback `go test` target when neither the
// --package flag nor the interactive picker narrows the run. It expands via
// `go list` to every package under integration-tests/k8s/tests/.
const defaultTestPackages = "./integration-tests/k8s/tests/..."

type config struct {
	repoRoot       string
	kubeconfig     string
	alloyImage     string
	reuseCluster   bool
	skipAlloyBuild bool
	shard          string
	packages       []string
	runRegex       string
	interactive    bool
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := requireCommands("docker", "kind", "kubectl", "helm", "go", "make"); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := os.Chdir(cfg.repoRoot); err != nil {
		fmt.Printf("change dir: %v\n", err)
		os.Exit(1)
	}
	if cfg.interactive {
		if err := configureInteractive(&cfg); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.kubeconfig), 0o700); err != nil {
		fmt.Printf("create kubeconfig dir: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		if cfg.reuseCluster {
			util.Logf("reuse mode enabled; keeping cluster %s", clusterName)
			return
		}
		_ = util.Step("delete kind cluster (post-test)", func() error {
			return harness.RunCommand("kind", "delete", "cluster", "--name", clusterName)
		})
	}()

	steps := []struct {
		name string
		fn   func() error
	}{
		{"build alloy image", func() error { return maybeBuildAlloyImage(cfg) }},
		{"ensure kind cluster", func() error { return ensureCluster(cfg) }},
		{"configure kubeconfig env", func() error { return configureEnvVariables(cfg) }},
		{"load alloy image into kind", func() error { return loadAlloyImage(cfg) }},
		{"run go tests", func() error { return runGoTests(cfg) }},
	}
	for _, s := range steps {
		if err := util.Step(s.name, s.fn); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}

func parseFlags() (config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config{}, err
	}
	repoRoot := wd
	kubeconfigPath := filepath.Join(repoRoot, "integration-tests", "k8s", ".kube", "kubeconfig")

	cfg := config{
		repoRoot:   repoRoot,
		kubeconfig: kubeconfigPath,
	}

	var pkgFlag string
	flag.CommandLine.SetOutput(os.Stdout)
	flag.BoolVar(&cfg.reuseCluster, "reuse-cluster", false, "Reuse the existing kind cluster and keep it after the run. The runner does NOT clean leftover namespaces, so a flaky previous run can fail with AlreadyExists; rerun without this flag to recreate the cluster from scratch")
	flag.BoolVar(&cfg.skipAlloyBuild, "skip-alloy-build", false, "Skip running make alloy-image; the image must already exist locally or in the kind cluster")
	flag.StringVar(&cfg.shard, "shard", "", "Split test packages across shards (e.g., 0/2)")
	flag.StringVar(&pkgFlag, "package", "", "Restrict tests to one package path or pattern (default: "+defaultTestPackages+")")
	flag.StringVar(&cfg.runRegex, "run", "", "Forward -run regex to `go test` (e.g. --run TestMimirAlerts to rerun a single test)")
	flag.StringVar(&cfg.alloyImage, "alloy-image", "grafana/alloy:latest", "Alloy image (repo:tag) used by tests; must exist locally or in the kind cluster")
	flag.BoolVar(&cfg.interactive, "interactive", false, "Pick run options (reuse-cluster, skip-alloy-build, shard/packages) via an interactive menu before running")
	flag.Usage = func() {
		fmt.Println("Usage: go run ./integration-tests/k8s/runner [flags]")
		fmt.Println()
		flag.PrintDefaults()
	}
	flag.Parse()
	if pkgFlag != "" {
		cfg.packages = []string{pkgFlag}
	}
	return cfg, nil
}

func requireCommands(commands ...string) error {
	for _, c := range commands {
		if _, err := exec.LookPath(c); err != nil {
			return fmt.Errorf("missing required command: %s", c)
		}
	}
	return nil
}

func maybeBuildAlloyImage(cfg config) error {
	if !cfg.skipAlloyBuild {
		return harness.RunCommand("make", "alloy-image")
	}
	util.Logf("--skip-alloy-build set; expecting %q already in local docker daemon", cfg.alloyImage)
	return harness.RunCommandQuiet("docker", "image", "inspect", cfg.alloyImage)
}

func ensureCluster(cfg config) error {
	exists, err := clusterExists()
	if err != nil {
		return err
	}
	if exists {
		if cfg.reuseCluster {
			util.Logf("reusing existing cluster %s", clusterName)
			return nil
		}
		util.Logf("cluster already exists, deleting stale cluster first")
		if err := harness.RunCommand("kind", "delete", "cluster", "--name", clusterName); err != nil {
			return err
		}
	}
	return harness.RunCommand("kind", "create", "cluster", "--name", clusterName)
}

func clusterExists() (bool, error) {
	cmd := exec.Command("kind", "get", "clusters")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, fmt.Errorf("kind get clusters failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return false, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == clusterName {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func configureEnvVariables(cfg config) error {
	// 0o600: kubeconfig holds cluster credentials; kubectl emits an
	// "insecure permissions" warning if it's readable by group/other.
	file, err := os.OpenFile(cfg.kubeconfig, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create kubeconfig: %w", err)
	}
	defer file.Close()

	cmd := exec.Command("kind", "get", "kubeconfig", "--name", clusterName)
	cmd.Stdout = file
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind get kubeconfig: %w", err)
	}

	if err := os.Setenv(managedEnv, "1"); err != nil {
		return err
	}
	if err := os.Setenv(kubeconfigEnv, cfg.kubeconfig); err != nil {
		return err
	}
	if err := os.Setenv(alloyImageEnv, cfg.alloyImage); err != nil {
		return err
	}
	return os.Setenv(kindClusterNameEnv, clusterName)
}

// loadAlloyImage loads the Alloy image (the artifact under test) into the kind
// cluster. Test-specific images (prom-gen, blackbox-exporter, etc.) are the
// responsibility of their respective dependencies in deps/.
func loadAlloyImage(cfg config) error {
	return harness.RunCommand("kind", "load", "docker-image", cfg.alloyImage, "--name", clusterName)
}

// runGoTests runs `go test -v` once for the configured patterns. With a
// single package go test streams logs line-by-line; with multiple packages
// (e.g. the default `./...` wildcard) it runs them in parallel up to
// GOMAXPROCS and prints each package's output once that package finishes.
func runGoTests(cfg config) error {
	patterns := cfg.packages
	if len(patterns) == 0 {
		patterns = []string{defaultTestPackages}
	}
	args := []string{"test", "-v", "-timeout", "30m"}
	if cfg.runRegex != "" {
		args = append(args, "-run", cfg.runRegex)
	}
	args = append(args, patterns...)
	if cfg.shard != "" {
		args = append(args, "-args", "-shard="+cfg.shard)
	}
	return util.Step("go test "+strings.Join(patterns, " "), func() error {
		return harness.RunCommand("go", args...)
	})
}
