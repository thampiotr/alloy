package build

import (
	"github.com/grafana/alloy/internal/component/discovery"
	"github.com/grafana/alloy/internal/component/prometheus/exporter/memcached"
	"github.com/grafana/alloy/internal/converter/internal/common"
	"github.com/grafana/alloy/internal/static/integrations/memcached_exporter"
)

func (b *ConfigBuilder) appendMemcachedExporter(config *memcached_exporter.Config, instanceKey *string) discovery.Exports {
	args := toMemcachedExporter(config)
	return b.appendExporterBlock(args, config.Name(), instanceKey, "memcached")
}

func toMemcachedExporter(config *memcached_exporter.Config) *memcached.Arguments {
	return &memcached.Arguments{
		Address:   config.MemcachedAddress,
		Timeout:   config.Timeout,
		TLSConfig: common.ToTLSConfig(config.TLSConfig),
	}
}
