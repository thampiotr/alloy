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
		if err := runInteractive(&cfg); err != nil {
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
			logf("reuse mode enabled; keeping cluster %s", clusterName)
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
		{"configure kubeconfig env", func() error { return configureKubeEnv(cfg) }},
		{"clean reused cluster namespaces", func() error { return cleanReusedClusterNamespaces(cfg) }},
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
	kubeconfigPath := filepath.Join(repoRoot, "integration-tests", "k8s", ".tmp", "kubeconfig")

	cfg := config{
		repoRoot:   repoRoot,
		kubeconfig: kubeconfigPath,
	}

	var pkgFlag string
	flag.CommandLine.SetOutput(os.Stdout)
	flag.BoolVar(&cfg.reuseCluster, "reuse-cluster", false, "Reuse fixed kind cluster and keep it after test run; without this flag the runner deletes any existing cluster up-front")
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
	logf("--skip-alloy-build set; expecting %q already in local docker daemon", cfg.alloyImage)
	return harness.RunCommandQuiet("docker", "image", "inspect", cfg.alloyImage)
}

func ensureCluster(cfg config) error {
	exists, err := clusterExists()
	if err != nil {
		return err
	}
	if exists {
		if cfg.reuseCluster {
			logf("reusing existing cluster %s", clusterName)
			return nil
		}
		logf("cluster already exists, deleting stale cluster first")
		if err := harness.RunCommand("kind", "delete", "cluster", "--name", clusterName); err != nil {
			return err
		}
	}
	return harness.RunCommand("kind", "create", "cluster", "--name", clusterName)
}

// systemNamespaces are namespaces that ship with kind/Kubernetes itself or
// that the runner installs cluster-wide (e.g. the prometheus-operator
// Deployment lives in `default`). They are never deleted by
// cleanReusedClusterNamespaces.
var systemNamespaces = map[string]struct{}{
	"default":            {},
	"kube-system":        {},
	"kube-public":        {},
	"kube-node-lease":    {},
	"local-path-storage": {},
}

// cleanReusedClusterNamespaces is a safety net for --reuse-cluster: when the
// kind cluster is being reused, any non-system namespaces from a previous
// (possibly aborted) run can produce "AlreadyExists" errors during install.
// We list them, ask the user to confirm, then delete them. No-op without
// --reuse-cluster, on a fresh cluster, or when no leftover namespaces exist.
func cleanReusedClusterNamespaces(cfg config) error {
	if !cfg.reuseCluster {
		return nil
	}
	leftovers, err := listNonSystemNamespaces(cfg.kubeconfig)
	if err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}
	if len(leftovers) == 0 {
		return nil
	}
	fmt.Println("[k8s-itest] reuse-cluster: the following non-system namespaces are leftover from a previous run and will be deleted before tests start:")
	for _, ns := range leftovers {
		fmt.Printf("  - %s\n", ns)
	}
	fmt.Print("[k8s-itest] proceed? (y/N) ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return errors.New("aborted by user; rerun without --reuse-cluster or clean the cluster manually")
	}
	for _, ns := range leftovers {
		logf("deleting namespace %s", ns)
		if err := harness.RunCommand("kubectl", "--kubeconfig", cfg.kubeconfig,
			"delete", "namespace", ns, "--wait=true", "--timeout=10m",
		); err != nil {
			return fmt.Errorf("delete namespace %q: %w", ns, err)
		}
	}
	return nil
}

func listNonSystemNamespaces(kubeconfig string) ([]string, error) {
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig,
		"get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}",
	)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("kubectl get namespaces: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	var leftovers []string
	for _, ns := range strings.Fields(string(out)) {
		if _, isSystem := systemNamespaces[ns]; isSystem {
			continue
		}
		leftovers = append(leftovers, ns)
	}
	return leftovers, nil
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

func configureKubeEnv(cfg config) error {
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

func logf(format string, args ...any) {
	fmt.Printf("[k8s-itest] "+format+"\n", args...)
}
