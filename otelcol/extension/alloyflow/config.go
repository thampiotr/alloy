package alloyflow

// Config represents the configuration for the alloy-flow extension.
type Config struct {

	// Endpoint specifies the alloy-flow endpoint to connect to.
	// Default: "localhost:8080"
	Endpoint string `mapstructure:"endpoint"`

	// Timeout specifies the timeout for alloy-flow operations.
	// Default: 30s
	Timeout string `mapstructure:"timeout"`

	// EnableDebug enables debug logging for the extension.
	// Default: false
	EnableDebug bool `mapstructure:"enable_debug"`
}

// Validate checks if the extension configuration is valid.
func (cfg *Config) Validate() error {
	// Add validation logic here if needed
	return nil
}
