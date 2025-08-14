package alloyflow

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"
)

// alloyFlowExtension implements the alloy-flow extension.
type alloyFlowExtension struct {
	config   *Config
	settings component.TelemetrySettings

	// Add fields for maintaining connection state, etc.
	endpoint string
	timeout  time.Duration
	debug    bool
}

// newAlloyFlowExtension creates a new alloy-flow extension instance.
func newAlloyFlowExtension(config *Config, settings component.TelemetrySettings) *alloyFlowExtension {
	timeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		timeout = 30 * time.Second // default timeout
	}

	return &alloyFlowExtension{
		config:   config,
		settings: settings,
		endpoint: config.Endpoint,
		timeout:  timeout,
		debug:    config.EnableDebug,
	}
}

// Start is called when the extension is started.
func (e *alloyFlowExtension) Start(ctx context.Context, host component.Host) error {
	if e.debug {
		e.settings.Logger.Info("Starting alloy-flow extension",
			zap.String("endpoint", e.endpoint),
			zap.Duration("timeout", e.timeout),
		)
	}

	// Initialize connection to alloy-flow here
	// For now, this is just a placeholder
	e.settings.Logger.Info("alloy-flow extension started successfully")
	return nil
}

// Shutdown is called when the extension is being stopped.
func (e *alloyFlowExtension) Shutdown(ctx context.Context) error {
	if e.debug {
		e.settings.Logger.Info("Shutting down alloy-flow extension")
	}

	// Clean up resources here
	// For now, this is just a placeholder
	e.settings.Logger.Info("alloy-flow extension shut down successfully")
	return nil
}

// Ready returns nil when the extension is ready to process data.
func (e *alloyFlowExtension) Ready() error {
	// Check if the extension is ready to handle requests
	// For now, always return ready
	return nil
}

// NotReady returns an error when the extension is not ready to process data.
func (e *alloyFlowExtension) NotReady() error {
	return fmt.Errorf("alloy-flow extension is not ready")
}
