package flowcmd

import (
	"github.com/grafana/alloy/internal/alloycli"
	"github.com/spf13/cobra"
)

// Command exposes the root Cobra command constructed by the internal alloy CLI.
func Command() *cobra.Command {
	return alloycli.Command()
}
