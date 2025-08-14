package alloyflow

import (
	"context"

	"go.opentelemetry.io/collector/component"
)

// alloyFlowExtension implements the alloyflow extension.
type alloyFlowExtension struct {
	config   *Config
	settings component.TelemetrySettings
	debug    bool
}

// newAlloyFlowExtension creates a new alloyflow extension instance.
func newAlloyFlowExtension(config *Config, settings component.TelemetrySettings) *alloyFlowExtension {
	return &alloyFlowExtension{
		config:   config,
		settings: settings,
		debug:    config.EnableDebug,
	}
}

// Start is called when the extension is started.
func (e *alloyFlowExtension) Start(ctx context.Context, host component.Host) error {
	if e.debug {
		e.settings.Logger.Info("Starting alloyflow extension")
	}

	// Initialize connection to alloyflow here
	// For now, this is just a placeholder
	e.settings.Logger.Info("alloyflow extension started successfully")
	return nil
}

// Shutdown is called when the extension is being stopped.
func (e *alloyFlowExtension) Shutdown(ctx context.Context) error {
	if e.debug {
		e.settings.Logger.Info("Shutting down alloyflow extension")
	}

	// Clean up resources here
	// For now, this is just a placeholder
	e.settings.Logger.Info("alloyflow extension shut down successfully")
	return nil
}

// Ready returns nil when the extension is ready to process data.
func (e *alloyFlowExtension) Ready() error {
	// The extension is always ready
	return nil
}

// NotReady returns an error when the extension is not ready to process data.
func (e *alloyFlowExtension) NotReady() error {
	// The extension is always ready, so this should never return an error
	return nil
}
