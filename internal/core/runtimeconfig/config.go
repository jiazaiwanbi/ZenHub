package runtimeconfig

import (
	"crypto/sha256"
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

type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
	ProtocolGemini    Protocol = "gemini"
)

type File struct {
	Providers      []Provider          `json:"providers,omitempty"`
	Listen         string              `json:"listen"`
	Routes         []Route             `json:"routes,omitempty"`
	ProviderGroups []ProviderGroup     `json:"provider_groups,omitempty"`
	Observability  ObservabilityConfig `json:"observability"`
	Sync           SyncConfig          `json:"sync"`
	Codex          CodexSettings       `json:"codex,omitempty"`
}

type Snapshot struct {
	Providers      []Provider      `json:"providers,omitempty"`
	Routes         []Route         `json:"routes,omitempty"`
	ProviderGroups []ProviderGroup `json:"provider_groups,omitempty"`
}

type Provider struct {
	Name          string               `json:"name"`
	Protocol      Protocol             `json:"protocol"`
	Mode          string               `json:"mode,omitempty"`
	BaseURL       string               `json:"base_url"`
	APIKey        string               `json:"api_key,omitempty"`
	APIKeyEnv     string               `json:"api_key_env,omitempty"`
	Headers       map[string]string    `json:"headers,omitempty"`
	Timeout       Duration             `json:"timeout,omitempty"`
	RetryCount    int                  `json:"retry_count,omitempty"`
	PassiveHealth PassiveHealthConfig  `json:"passive_health,omitempty"`
	Codex         *CodexProviderConfig `json:"codex,omitempty"`
	Models        []ProviderModel      `json:"models"`
}

type ProviderModel struct {
	Alias     string `json:"alias"`
	RealModel string `json:"real_model"`
	Weight    int    `json:"weight,omitempty"`
}

type Route struct {
	Model         string `json:"model"`
	Mode          string `json:"mode"`
	ProviderGroup string `json:"provider_group"`
	UpstreamModel string `json:"upstream_model"`
}

type ProviderGroup struct {
	Name            string               `json:"name"`
	Protocol        string               `json:"protocol"`
	Strategy        string               `json:"strategy"`
	Timeout         Duration             `json:"timeout"`
	RetryCount      int                  `json:"retry_count"`
	MaxNodeAttempts int                  `json:"max_node_attempts"`
	PassiveHealth   PassiveHealthConfig  `json:"passive_health"`
	Nodes           []Node               `json:"nodes"`
	Codex           *CodexProviderConfig `json:"codex,omitempty"`
}

type CodexSettings struct {
	ConfigDir            string `json:"config_dir,omitempty"`
	CurrentProviderGroup string `json:"current_provider_group,omitempty"`
}

type CodexProviderConfig struct {
	Auth   json.RawMessage `json:"auth,omitempty"`
	Config string          `json:"config,omitempty"`
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

type SyncConfig struct {
	Enabled     bool   `json:"enabled"`
	ServerURL   string `json:"server_url"`
	Username    string `json:"username"`
	UsernameEnv string `json:"username_env"`
	Password    string `json:"password"`
	PasswordEnv string `json:"password_env"`
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
	return NormalizeFile(file), nil
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
	snapshot := Snapshot{
		Providers:      append([]Provider(nil), f.Providers...),
		Routes:         append([]Route(nil), f.Routes...),
		ProviderGroups: append([]ProviderGroup(nil), f.ProviderGroups...),
	}
	return NormalizeSnapshot(snapshot)
}

func NormalizeFile(file File) File {
	if len(file.Providers) == 0 {
		return file
	}

	normalized := NormalizeSnapshot(Snapshot{
		Providers:      append([]Provider(nil), file.Providers...),
		Routes:         append([]Route(nil), file.Routes...),
		ProviderGroups: append([]ProviderGroup(nil), file.ProviderGroups...),
	})
	file.Providers = normalized.Providers
	file.Routes = normalized.Routes
	file.ProviderGroups = normalized.ProviderGroups
	return file
}

func NormalizeSnapshot(snapshot Snapshot) Snapshot {
	if len(snapshot.Providers) == 0 {
		return snapshot
	}

	routes, groups, err := compileProviders(snapshot.Providers)
	if err != nil {
		return snapshot
	}
	snapshot.Routes = routes
	snapshot.ProviderGroups = groups
	return Snapshot{
		Providers:      append([]Provider(nil), snapshot.Providers...),
		Routes:         append([]Route(nil), snapshot.Routes...),
		ProviderGroups: append([]ProviderGroup(nil), snapshot.ProviderGroups...),
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

	snapshot = NormalizeSnapshot(snapshot)
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
		Protocol:        strings.TrimSpace(group.Protocol),
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

func compileProviders(providers []Provider) ([]Route, []ProviderGroup, error) {
	if len(providers) == 0 {
		return nil, nil, nil
	}

	routes := make([]Route, 0, len(providers))
	groups := make([]ProviderGroup, 0, len(providers))
	seenProviders := make(map[string]bool, len(providers))

	for _, provider := range providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			return nil, nil, errors.New("provider name is required")
		}
		if seenProviders[name] {
			return nil, nil, fmt.Errorf("duplicate provider %q", name)
		}
		seenProviders[name] = true

		protocol := normalizeProviderProtocol(provider.Protocol)
		if protocol == "" {
			return nil, nil, fmt.Errorf("provider %q protocol is required", name)
		}
		if strings.TrimSpace(provider.BaseURL) == "" {
			return nil, nil, fmt.Errorf("provider %q base_url is required", name)
		}
		if len(provider.Models) == 0 {
			return nil, nil, fmt.Errorf("provider %q requires at least one model mapping", name)
		}

		group := ProviderGroup{
			Name:            name,
			Protocol:        string(protocol),
			Strategy:        string(balancer.StrategyRoundRobin),
			Timeout:         provider.Timeout,
			RetryCount:      provider.RetryCount,
			MaxNodeAttempts: 1,
			PassiveHealth:   provider.PassiveHealth,
			Nodes: []Node{
				{
					Name:      name,
					BaseURL:   strings.TrimSpace(provider.BaseURL),
					APIKey:    strings.TrimSpace(provider.APIKey),
					APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
					Headers:   cloneHeaders(provider.Headers),
				},
			},
			Codex: provider.Codex,
		}
		groups = append(groups, group)

		mode := strings.TrimSpace(provider.Mode)
		if mode == "" {
			mode = string(router.RouteModeDirect)
		}

		for _, model := range provider.Models {
			alias := strings.TrimSpace(model.Alias)
			realModel := strings.TrimSpace(model.RealModel)
			if alias == "" {
				return nil, nil, fmt.Errorf("provider %q has empty alias", name)
			}
			if realModel == "" {
				return nil, nil, fmt.Errorf("provider %q alias %q has empty real_model", name, alias)
			}

			weight := model.Weight
			if weight <= 0 {
				weight = 1
			}
			for i := 0; i < weight; i++ {
				routes = append(routes, Route{
					Model:         alias,
					Mode:          mode,
					ProviderGroup: name,
					UpstreamModel: realModel,
				})
			}
		}
	}

	return routes, groups, nil
}

func normalizeProviderProtocol(protocol Protocol) Protocol {
	switch Protocol(strings.ToLower(strings.TrimSpace(string(protocol)))) {
	case ProtocolOpenAI:
		return ProtocolOpenAI
	case ProtocolAnthropic:
		return ProtocolAnthropic
	case ProtocolGemini:
		return ProtocolGemini
	default:
		return ""
	}
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

func HashSnapshot(snapshot Snapshot) (string, error) {
	raw, err := json.Marshal(NormalizeSnapshot(snapshot))
	if err != nil {
		return "", fmt.Errorf("marshal snapshot for hashing: %w", err)
	}

	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:]), nil
}
