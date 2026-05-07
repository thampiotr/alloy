package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// RunCommand runs name with args, inheriting stdout/stderr and the managed
// test kubeconfig via commandEnv.
func RunCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = CommandEnv()
	return cmd.Run()
}

// RunCommandQuiet runs name with args and discards stdout/stderr. Use it for
// idempotency checks (e.g. `docker image inspect`) where the success/failure
// of the command is the signal and the output would only add noise.
func RunCommandQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = CommandEnv()
	return cmd.Run()
}

// RunCommandStdin runs name with args, piping the given content as stdin.
// stdout and stderr are inherited; the managed test kubeconfig is set via
// CommandEnv. Useful for `kubectl apply -f -` style invocations.
func RunCommandStdin(stdin, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = CommandEnv()
	return cmd.Run()
}

// runDiagnosticCommand runs name with args under ctx and prints the combined
// output. It is intended for failure-diagnostics hooks and never inherits
// stdin. Errors are returned with context-aware timeout reporting.
func runDiagnosticCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = CommandEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if out.Len() > 0 {
		fmt.Printf("%s", out.String())
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s %v timed out: %w", name, args, ctx.Err())
	}
	return fmt.Errorf("%s %v failed: %w", name, args, err)
}

// RunDiagnosticCommands runs each command in turn, accumulating errors so a
// single command's failure does not skip the rest. Returns a joined error
// (or nil) for the caller to log.
func RunDiagnosticCommands(ctx context.Context, commands [][]string) error {
	var errs []string
	for _, args := range commands {
		if len(args) == 0 {
			continue
		}
		if err := runDiagnosticCommand(ctx, args[0], args[1:]...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
