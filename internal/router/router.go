package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"zenhub/internal/canonical"
)

type RouteMode string

const (
	RouteModeDirect RouteMode = "direct"
	RouteModeRelay  RouteMode = "relay"
)

var ErrNoRoute = errors.New("no route for model")

type Rule struct {
	Model         string
	Mode          RouteMode
	ProviderGroup string
	UpstreamModel string
}

type Decision struct {
	Model         string
	Mode          RouteMode
	ProviderGroup string
	UpstreamModel string
}

type Router struct {
	rules  map[string]Rule
	models []string
}

func New(rules []Rule) (*Router, error) {
	if len(rules) == 0 {
		return nil, errors.New("at least one route rule is required")
	}

	byModel := make(map[string]Rule, len(rules))
	models := make([]string, 0, len(rules))
	for _, rule := range rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			return nil, errors.New("route model is required")
		}
		rule.Model = model
		if rule.Mode == "" {
			rule.Mode = RouteModeDirect
		}
		if rule.Mode != RouteModeDirect && rule.Mode != RouteModeRelay {
			return nil, fmt.Errorf("route %q has unsupported mode %q", rule.Model, rule.Mode)
		}
		if strings.TrimSpace(rule.ProviderGroup) == "" {
			return nil, fmt.Errorf("route %q provider group is required", rule.Model)
		}
		if _, exists := byModel[rule.Model]; exists {
			return nil, fmt.Errorf("duplicate route for model %q", rule.Model)
		}
		byModel[rule.Model] = rule
		models = append(models, rule.Model)
	}
	sort.Strings(models)

	return &Router{rules: byModel, models: models}, nil
}

func (r *Router) Select(req canonical.ChatRequest) (Decision, error) {
	rule, ok := r.rules[req.Model]
	if !ok {
		return Decision{}, fmt.Errorf("%w %q", ErrNoRoute, req.Model)
	}
	return Decision{
		Model:         rule.Model,
		Mode:          rule.Mode,
		ProviderGroup: rule.ProviderGroup,
		UpstreamModel: rule.UpstreamModel,
	}, nil
}

func (r *Router) Models() []string {
	return append([]string(nil), r.models...)
}
