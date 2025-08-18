package alloyflow

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/grafana/alloy/flowcmd"
)

var _ extension.Extension = (*alloyFlowExtension)(nil)

// alloyFlowExtension implements the alloyflow extension.
type alloyFlowExtension struct {
	config   *Config
	settings component.TelemetrySettings

	shutdownOnce sync.Once
	runCancel    context.CancelFunc
	runExited    chan struct{}
}

// newAlloyFlowExtension creates a new alloyflow extension instance.
func newAlloyFlowExtension(config *Config, settings component.TelemetrySettings) *alloyFlowExtension {
	return &alloyFlowExtension{
		config:   config,
		settings: settings,
	}
}

// Start is called when the extension is started.
func (e *alloyFlowExtension) Start(ctx context.Context, host component.Host) error {
	if e.runCancel != nil {
		e.settings.Logger.Warn("alloyflow extension already started")
		return nil
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	e.runCancel = runCancel
	e.runExited = make(chan struct{})

	runCommand := flowcmd.RunCommand()
	runCommand.ParseFlags(e.config.flagsAsSlice())
	runCommand.SetArgs([]string{e.config.ConfigPath})
	go func() {
		defer close(e.runExited)		
		err := runCommand.ExecuteContext(runCtx)
		if err != nil {
			e.settings.Logger.Error("run command exited with an error", zap.Error(err))
		}
	}()

	e.settings.Logger.Info("alloyflow extension started successfully")
	return nil
}

// Shutdown is called when the extension is being stopped.
func (e *alloyFlowExtension) Shutdown(ctx context.Context) error {
	e.shutdownOnce.Do(func() {
		if e.runCancel == nil {
			e.settings.Logger.Info("alloyflow extension shut down (not started)")
			return
		}

		e.runCancel()
		e.runCancel = nil
		select {
		case <-e.runExited:
			e.settings.Logger.Info("alloyflow extension shut down successfully")
		case <-ctx.Done():
			e.settings.Logger.Warn("alloyflow extension shutdown interrupted by context", zap.Error(ctx.Err()))
		}
	})
	return nil
}

// Ready returns nil when the extension is ready to process data.
func (e *alloyFlowExtension) Ready() error {
	if e.runCancel == nil {
		return fmt.Errorf("alloyflow extension not started")
	}
	return nil
}

// NotReady returns an error when the extension is not ready to process data.
func (e *alloyFlowExtension) NotReady() error {
	if e.runCancel == nil {
		return fmt.Errorf("alloyflow extension not started")
	}
	return nil
}
