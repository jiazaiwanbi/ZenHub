package runtimeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"zenhub/internal/core/balancer"
	"zenhub/internal/core/router"
)

const (
	defaultListen             = "127.0.0.1:8080"
	defaultTimeout            = 30 * time.Second
	defaultHealthCooldown     = 30 * time.Second
	defaultObservabilityLimit = 100
)

type Duration struct {
	time.Duration
}

type File struct {
	Listen         string              `json:"listen"`
	Routes         []Route             `json:"routes"`
	ProviderGroups []ProviderGroup     `json:"provider_groups"`
	Observability  ObservabilityConfig `json:"observability"`
}

type Snapshot struct {
	Routes         []Route         `json:"routes"`
	ProviderGroups []ProviderGroup `json:"provider_groups"`
}

type Route struct {
	Model         string `json:"model"`
	Mode          string `json:"mode"`
	ProviderGroup string `json:"provider_group"`
	UpstreamModel string `json:"upstream_model"`
}

type ProviderGroup struct {
	Name            string              `json:"name"`
	Strategy        string              `json:"strategy"`
	Timeout         Duration            `json:"timeout"`
	RetryCount      int                 `json:"retry_count"`
	MaxNodeAttempts int                 `json:"max_node_attempts"`
	PassiveHealth   PassiveHealthConfig `json:"passive_health"`
	Nodes           []Node              `json:"nodes"`
}

type PassiveHealthConfig struct {
	FailureThreshold int      `json:"failure_threshold"`
	Cooldown         Duration `json:"cooldown"`
}

type Node struct {
	Name      string            `json:"name"`
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key"`
	APIKeyEnv string            `json:"api_key_env"`
	Headers   map[string]string `json:"headers"`
}

type ObservabilityConfig struct {
	MaxRecords int `json:"max_records"`
}

type Runtime struct {
	Listen             string
	Routes             []router.Rule
	ProviderGroups     []balancer.Group
	ObservabilityLimit int
}

func Load(path string) (Runtime, error) {
	file, err := LoadFile(path)
	if err != nil {
		return Runtime{}, err
	}
	return file.Runtime()
}

func LoadFile(path string) (File, error) {
	if strings.TrimSpace(path) == "" {
		return File{}, errors.New("config path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}

	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	return file, nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		d.Duration = 0
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a JSON string")
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

func (f File) Runtime() (Runtime, error) {
	return runtimeFromParts(f.Listen, f.Snapshot(), f.Observability.MaxRecords)
}

func (f File) Snapshot() Snapshot {
	return Snapshot{
		Routes:         append([]Route(nil), f.Routes...),
		ProviderGroups: append([]ProviderGroup(nil), f.ProviderGroups...),
	}
}

func (s Snapshot) Runtime(listen string, observabilityLimit int) (Runtime, error) {
	return runtimeFromParts(listen, s, observabilityLimit)
}

func (s Snapshot) Validate() error {
	_, err := runtimeFromParts(defaultListen, s, defaultObservabilityLimit)
	return err
}

func runtimeFromParts(listen string, snapshot Snapshot, observabilityLimit int) (Runtime, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = defaultListen
	}
	if len(snapshot.Routes) == 0 {
		return Runtime{}, errors.New("at least one route is required")
	}
	if len(snapshot.ProviderGroups) == 0 {
		return Runtime{}, errors.New("at least one provider group is required")
	}

	groups := make([]balancer.Group, 0, len(snapshot.ProviderGroups))
	groupNames := make(map[string]bool, len(snapshot.ProviderGroups))
	for _, group := range snapshot.ProviderGroups {
		converted, err := convertGroup(group)
		if err != nil {
			return Runtime{}, err
		}
		if groupNames[converted.Name] {
			return Runtime{}, fmt.Errorf("duplicate provider group %q", converted.Name)
		}
		groupNames[converted.Name] = true
		groups = append(groups, converted)
	}

	routes := make([]router.Rule, 0, len(snapshot.Routes))
	for _, route := range snapshot.Routes {
		converted := router.Rule{
			Model:         strings.TrimSpace(route.Model),
			Mode:          router.RouteMode(strings.TrimSpace(route.Mode)),
			ProviderGroup: strings.TrimSpace(route.ProviderGroup),
			UpstreamModel: strings.TrimSpace(route.UpstreamModel),
		}
		if !groupNames[converted.ProviderGroup] {
			return Runtime{}, fmt.Errorf("route %q references unknown provider group %q", converted.Model, converted.ProviderGroup)
		}
		routes = append(routes, converted)
	}

	if observabilityLimit <= 0 {
		observabilityLimit = defaultObservabilityLimit
	}

	return Runtime{
		Listen:             listen,
		Routes:             routes,
		ProviderGroups:     groups,
		ObservabilityLimit: observabilityLimit,
	}, nil
}

func convertGroup(group ProviderGroup) (balancer.Group, error) {
	timeout := group.Timeout.Duration
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	cooldown := group.PassiveHealth.Cooldown.Duration
	if cooldown <= 0 {
		cooldown = defaultHealthCooldown
	}

	failureThreshold := group.PassiveHealth.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = 1
	}

	nodes := make([]balancer.Node, 0, len(group.Nodes))
	for _, node := range group.Nodes {
		resolved := strings.TrimSpace(node.APIKey)
		if resolved == "" && strings.TrimSpace(node.APIKeyEnv) != "" {
			resolved = strings.TrimSpace(os.Getenv(strings.TrimSpace(node.APIKeyEnv)))
		}
		nodes = append(nodes, balancer.Node{
			Name:    strings.TrimSpace(node.Name),
			BaseURL: strings.TrimSpace(node.BaseURL),
			APIKey:  resolved,
			Headers: cloneHeaders(node.Headers),
		})
	}

	return balancer.Group{
		Name:            strings.TrimSpace(group.Name),
		Strategy:        balancer.Strategy(strings.TrimSpace(group.Strategy)),
		Timeout:         timeout,
		RetryCount:      group.RetryCount,
		MaxNodeAttempts: group.MaxNodeAttempts,
		PassiveHealth: balancer.PassiveHealth{
			FailureThreshold: failureThreshold,
			Cooldown:         cooldown,
		},
		Nodes: nodes,
	}, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}
