package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"zenhub/internal/core/canonical"
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
	Key           string
	Model         string
	Mode          RouteMode
	ProviderGroup string
	UpstreamModel string
}

type Router struct {
	mu     sync.Mutex
	pools  map[string]*poolState
	models []string
}

type poolState struct {
	rules     []Rule
	nextIndex int
}

func New(rules []Rule) (*Router, error) {
	if len(rules) == 0 {
		return nil, errors.New("at least one route rule is required")
	}

	byModel := make(map[string]*poolState, len(rules))
	modelSet := make(map[string]bool, len(rules))
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

		pool, exists := byModel[rule.Model]
		if !exists {
			pool = &poolState{}
			byModel[rule.Model] = pool
		}
		pool.rules = append(pool.rules, rule)
		modelSet[rule.Model] = true
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)

	return &Router{pools: byModel, models: models}, nil
}

func (r *Router) Select(req canonical.ChatRequest) (Decision, error) {
	return r.SelectWithExclusions(req, nil)
}

func (r *Router) SelectWithExclusions(req canonical.ChatRequest, excluded map[string]bool) (Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pool, ok := r.pools[req.Model]
	if !ok {
		return Decision{}, fmt.Errorf("%w %q", ErrNoRoute, req.Model)
	}
	for offset := 0; offset < len(pool.rules); offset++ {
		idx := (pool.nextIndex + offset) % len(pool.rules)
		key := decisionKey(req.Model, idx)
		if excluded != nil && excluded[key] {
			continue
		}

		rule := pool.rules[idx]
		pool.nextIndex = (idx + 1) % len(pool.rules)
		return Decision{
			Key:           key,
			Model:         rule.Model,
			Mode:          rule.Mode,
			ProviderGroup: rule.ProviderGroup,
			UpstreamModel: rule.UpstreamModel,
		}, nil
	}

	return Decision{}, fmt.Errorf("%w %q", ErrNoRoute, req.Model)
}

func (r *Router) Models() []string {
	return append([]string(nil), r.models...)
}

func decisionKey(model string, index int) string {
	return fmt.Sprintf("%s#%d", model, index)
}
