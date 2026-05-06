package config

import coreconfig "zenhub/internal/core/runtimeconfig"

type Duration = coreconfig.Duration
type File = coreconfig.File
type Snapshot = coreconfig.Snapshot
type Route = coreconfig.Route
type ProviderGroup = coreconfig.ProviderGroup
type PassiveHealthConfig = coreconfig.PassiveHealthConfig
type Node = coreconfig.Node
type ObservabilityConfig = coreconfig.ObservabilityConfig
type Runtime = coreconfig.Runtime

func Load(path string) (Runtime, error) {
	return coreconfig.Load(path)
}
