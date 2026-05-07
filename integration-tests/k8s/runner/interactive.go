package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
)

// runInteractive presents a small TUI form letting the developer pick the
// commonly tweaked runner options (reuse cluster, skip alloy image, filter by
// shard or by packages) and writes the choices into cfg before tests run.
//
// Both "reuse cluster" and "skip alloy image" default to selected because
// that's the typical dev-machine flow; the user can deselect them in the form.
func runInteractive(cfg *config) error {
	runOpts := []string{"reuse-cluster", "skip-alloy-image"}
	filterMode := "shard"
	shard := cfg.shard
	if shard == "" {
		shard = "0/1"
	}

	pkgs, err := discoverTestPackages(cfg.repoRoot)
	if err != nil {
		return fmt.Errorf("discover test packages: %w", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no test packages found under integration-tests/k8s/tests/")
	}
	pickedPkgs := []string{pkgs[0]} // default to first so the form is valid

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Run options").
				Description("Space toggles, Enter confirms.").
				Options(
					huh.NewOption("Reuse kind cluster", "reuse-cluster").Selected(true),
					huh.NewOption("Skip Alloy image build", "skip-alloy-image").Selected(true),
				).
				Value(&runOpts),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How do you want to filter tests?").
				Options(
					huh.NewOption("Shard like CI (e.g. 0/2)", "shard"),
					huh.NewOption("Pick test packages", "packages"),
				).
				Value(&filterMode),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Shard (i/n)").
				Description("Index/total. 0/1 runs every package.").
				Value(&shard).
				Validate(validateShard),
		).WithHideFunc(func() bool { return filterMode != "shard" }),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Test packages to run").
				Options(buildPkgOptions(pkgs)...).
				Value(&pickedPkgs).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return fmt.Errorf("pick at least one package")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return filterMode != "packages" }),
	)
	if err := form.Run(); err != nil {
		return err
	}

	cfg.reuseCluster = slices.Contains(runOpts, "reuse-cluster")
	cfg.skipAlloy = slices.Contains(runOpts, "skip-alloy-image")
	switch filterMode {
	case "shard":
		// "0/1" is the no-op that runs everything; treat it as no shard so we
		// don't drag the shard tag through diagnostics output.
		if shard == "0/1" {
			cfg.shard = ""
		} else {
			cfg.shard = shard
		}
	case "packages":
		cfg.packages = pickedPkgs
		cfg.shard = ""
	}
	return nil
}

func discoverTestPackages(repoRoot string) ([]string, error) {
	root := filepath.Join(repoRoot, "integration-tests", "k8s", "tests")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgs = append(pkgs, "./integration-tests/k8s/tests/"+e.Name())
	}
	sort.Strings(pkgs)
	return pkgs, nil
}

func buildPkgOptions(pkgs []string) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(pkgs))
	for _, p := range pkgs {
		// Display the package's leaf directory; the value carries the full path.
		opts = append(opts, huh.NewOption(filepath.Base(p), p))
	}
	return opts
}

func validateShard(s string) error {
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("expected i/n, got %q", s)
	}
	return nil
}
