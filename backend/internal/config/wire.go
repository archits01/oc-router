package config

import "github.com/google/wire"

// ProviderSet
var ProviderSet = wire.NewSet(
	ProvideConfig,
)

// ProvideConfig
func ProvideConfig() (*Config, error) {
	return LoadForBootstrap()
}
