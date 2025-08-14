# Alloy Flow Extension

This is a scaffolding extension for integrating Alloy Flow with the OpenTelemetry Collector.

## Configuration

The following configuration options are available:

```yaml
extensions:
  alloyflow:
    endpoint: "localhost:8080"  # Alloy Flow endpoint (default: localhost:8080)
    timeout: "30s"              # Operation timeout (default: 30s)
    enable_debug: false         # Enable debug logging (default: false)
```

## Usage

Add the extension to your OpenTelemetry Collector configuration:

```yaml
extensions:
  alloyflow:
    endpoint: "localhost:8080"
    enable_debug: true

service:
  extensions: [alloyflow]
  pipelines:
    # your pipelines here
```

## Development Status

This extension is currently in development and serves as a scaffolding for future implementation.
