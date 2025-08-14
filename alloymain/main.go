package main

import (
	"github.com/grafana/alloy/flowcli"
)

func init() {
	// If the build version wasn't set by the build process, we'll set it based
	// on the version string in VERSION.
	version := flowcli.BuildVersion()
	if version == "" || version == "v0.0.0" {
		flowcli.SetBuildVersion(fallbackVersion())
	}
}

func main() {
	flowcli.Main()
}
