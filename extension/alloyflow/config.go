package alloyflow

// Config represents the configuration for the alloyflow extension.
type Config struct {
	// EnableDebug enables debug logging for the extension.
	// Default: false
	EnableDebug bool `mapstructure:"enable_debug"`
}

// Validate checks if the extension configuration is valid.
func (cfg *Config) Validate() error {
	// Add validation logic here if needed
	return nil
}
