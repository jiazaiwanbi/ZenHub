package controlplane

import (
	"encoding/json"
	"fmt"
	"time"

	"zenhub/internal/core/runtimeconfig"
	controlv1 "zenhub/internal/gen/controlv1"
)

func ToProtoSnapshot(snapshot runtimeconfig.Snapshot) *controlv1.Snapshot {
	return &controlv1.Snapshot{
		Routes:         ToProtoRoutes(snapshot.Routes),
		ProviderGroups: ToProtoProviderGroups(snapshot.ProviderGroups),
	}
}

func FromProtoSnapshot(snapshot *controlv1.Snapshot) (runtimeconfig.Snapshot, error) {
	if snapshot == nil {
		return runtimeconfig.Snapshot{}, nil
	}

	routes := make([]runtimeconfig.Route, 0, len(snapshot.Routes))
	for _, route := range snapshot.Routes {
		routes = append(routes, runtimeconfig.Route{
			Model:         route.GetModel(),
			Mode:          route.GetMode(),
			ProviderGroup: route.GetProviderGroup(),
			UpstreamModel: route.GetUpstreamModel(),
		})
	}

	providerGroups, err := FromProtoProviderGroups(snapshot.ProviderGroups)
	if err != nil {
		return runtimeconfig.Snapshot{}, err
	}

	return runtimeconfig.Snapshot{
		Routes:         routes,
		ProviderGroups: providerGroups,
	}, nil
}

func ToProtoRoutes(routes []runtimeconfig.Route) []*controlv1.Route {
	if len(routes) == 0 {
		return nil
	}
	result := make([]*controlv1.Route, 0, len(routes))
	for _, route := range routes {
		result = append(result, &controlv1.Route{
			Model:         route.Model,
			Mode:          route.Mode,
			ProviderGroup: route.ProviderGroup,
			UpstreamModel: route.UpstreamModel,
		})
	}
	return result
}

func ToProtoProviderGroups(groups []runtimeconfig.ProviderGroup) []*controlv1.ProviderGroup {
	if len(groups) == 0 {
		return nil
	}
	result := make([]*controlv1.ProviderGroup, 0, len(groups))
	for _, group := range groups {
		var codex *controlv1.CodexProviderConfig
		if group.Codex != nil {
			codex = &controlv1.CodexProviderConfig{
				AuthJson: cloneBytes(group.Codex.Auth),
				Config:   group.Codex.Config,
			}
		}

		var nodes []*controlv1.Node
		if len(group.Nodes) > 0 {
			nodes = make([]*controlv1.Node, 0, len(group.Nodes))
		}
		for _, node := range group.Nodes {
			var headers map[string]string
			if len(node.Headers) > 0 {
				headers = make(map[string]string, len(node.Headers))
				for key, value := range node.Headers {
					headers[key] = value
				}
			}
			nodes = append(nodes, &controlv1.Node{
				Name:      node.Name,
				BaseUrl:   node.BaseURL,
				ApiKey:    node.APIKey,
				ApiKeyEnv: node.APIKeyEnv,
				Headers:   headers,
			})
		}

		result = append(result, &controlv1.ProviderGroup{
			Name:            group.Name,
			Strategy:        group.Strategy,
			Timeout:         group.Timeout.Duration.String(),
			RetryCount:      int32(group.RetryCount),
			MaxNodeAttempts: int32(group.MaxNodeAttempts),
			PassiveHealth: &controlv1.PassiveHealthConfig{
				FailureThreshold: int32(group.PassiveHealth.FailureThreshold),
				Cooldown:         group.PassiveHealth.Cooldown.Duration.String(),
			},
			Nodes: nodes,
			Codex: codex,
		})
	}
	return result
}

func FromProtoProviderGroups(groups []*controlv1.ProviderGroup) ([]runtimeconfig.ProviderGroup, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	result := make([]runtimeconfig.ProviderGroup, 0, len(groups))
	for _, group := range groups {
		passiveHealth := group.GetPassiveHealth()
		timeout, err := parseDuration(group.GetTimeout())
		if err != nil {
			return nil, fmt.Errorf("parse provider group timeout for %q: %w", group.GetName(), err)
		}
		cooldown, err := parseDuration(passiveHealth.GetCooldown())
		if err != nil {
			return nil, fmt.Errorf("parse provider group cooldown for %q: %w", group.GetName(), err)
		}

		var nodes []runtimeconfig.Node
		if len(group.Nodes) > 0 {
			nodes = make([]runtimeconfig.Node, 0, len(group.Nodes))
		}
		for _, node := range group.Nodes {
			var headers map[string]string
			if len(node.Headers) > 0 {
				headers = make(map[string]string, len(node.Headers))
				for key, value := range node.Headers {
					headers[key] = value
				}
			}
			nodes = append(nodes, runtimeconfig.Node{
				Name:      node.GetName(),
				BaseURL:   node.GetBaseUrl(),
				APIKey:    node.GetApiKey(),
				APIKeyEnv: node.GetApiKeyEnv(),
				Headers:   headers,
			})
		}

		var codex *runtimeconfig.CodexProviderConfig
		if group.Codex != nil {
			codex = &runtimeconfig.CodexProviderConfig{
				Auth:   cloneRawMessage(group.Codex.GetAuthJson()),
				Config: group.Codex.GetConfig(),
			}
		}

		result = append(result, runtimeconfig.ProviderGroup{
			Name:            group.GetName(),
			Strategy:        group.GetStrategy(),
			Timeout:         runtimeconfig.Duration{Duration: timeout},
			RetryCount:      int(group.GetRetryCount()),
			MaxNodeAttempts: int(group.GetMaxNodeAttempts()),
			PassiveHealth: runtimeconfig.PassiveHealthConfig{
				FailureThreshold: int(passiveHealth.GetFailureThreshold()),
				Cooldown:         runtimeconfig.Duration{Duration: cooldown},
			},
			Nodes: nodes,
			Codex: codex,
		})
	}
	return result, nil
}

func parseDuration(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

func cloneBytes(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return cloned
}

func cloneRawMessage(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return cloned
}
