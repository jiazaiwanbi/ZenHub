package balancer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Strategy string

const (
	StrategyRoundRobin Strategy = "round_robin"
	StrategyFillFirst  Strategy = "fill_first"
)

var (
	ErrGroupNotFound  = errors.New("provider group not found")
	ErrNoHealthyNodes = errors.New("no healthy nodes available")
)

type PassiveHealth struct {
	FailureThreshold int
	Cooldown         time.Duration
}

type Node struct {
	Name    string
	BaseURL string
	APIKey  string
	Headers map[string]string
}

type Group struct {
	Name            string
	Strategy        Strategy
	Timeout         time.Duration
	RetryCount      int
	MaxNodeAttempts int
	PassiveHealth   PassiveHealth
	Nodes           []Node
}

type Selection struct {
	Group Group
	Node  Node
}

type Manager struct {
	mu     sync.Mutex
	now    func() time.Time
	groups map[string]*groupState
}

type groupState struct {
	cfg          Group
	nodes        []nodeState
	nextIndex    int
	currentIndex int
}

type nodeState struct {
	failures       int
	unhealthyUntil time.Time
}

func New(groups []Group) (*Manager, error) {
	return NewWithClock(groups, time.Now)
}

func NewWithClock(groups []Group, now func() time.Time) (*Manager, error) {
	if len(groups) == 0 {
		return nil, errors.New("at least one provider group is required")
	}
	if now == nil {
		now = time.Now
	}

	manager := &Manager{
		now:    now,
		groups: make(map[string]*groupState, len(groups)),
	}

	for _, group := range groups {
		normalized, err := normalizeGroup(group)
		if err != nil {
			return nil, err
		}
		if _, exists := manager.groups[normalized.Name]; exists {
			return nil, fmt.Errorf("duplicate provider group %q", normalized.Name)
		}
		manager.groups[normalized.Name] = &groupState{
			cfg:          normalized,
			nodes:        make([]nodeState, len(normalized.Nodes)),
			currentIndex: 0,
		}
	}

	return manager, nil
}

func (m *Manager) Group(name string) (Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[name]
	if !ok {
		return Group{}, fmt.Errorf("%w %q", ErrGroupNotFound, name)
	}
	return cloneGroup(group.cfg), nil
}

func (m *Manager) Select(name string, excluded map[string]bool) (Selection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, ok := m.groups[name]
	if !ok {
		return Selection{}, fmt.Errorf("%w %q", ErrGroupNotFound, name)
	}

	now := m.now()
	var idx int
	var found bool
	switch group.cfg.Strategy {
	case StrategyRoundRobin:
		idx, found = group.selectRoundRobin(now, excluded)
		if found {
			group.nextIndex = (idx + 1) % len(group.cfg.Nodes)
		}
	case StrategyFillFirst:
		idx, found = group.selectFillFirst(now, excluded)
		if found {
			group.currentIndex = idx
		}
	default:
		return Selection{}, fmt.Errorf("unsupported balancing strategy %q", group.cfg.Strategy)
	}

	if !found {
		return Selection{}, fmt.Errorf("%w in %q", ErrNoHealthyNodes, name)
	}

	return Selection{
		Group: cloneGroup(group.cfg),
		Node:  cloneNode(group.cfg.Nodes[idx]),
	}, nil
}

func (m *Manager) ReportSuccess(groupName, nodeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, node, idx := m.lookupNode(groupName, nodeName)
	if group == nil || node == nil {
		return
	}

	node.failures = 0
	node.unhealthyUntil = time.Time{}
	if group.cfg.Strategy == StrategyFillFirst {
		group.currentIndex = idx
	}
}

func (m *Manager) ReportFailure(groupName, nodeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group, node, idx := m.lookupNode(groupName, nodeName)
	if group == nil || node == nil {
		return
	}

	node.failures++
	if node.failures >= group.cfg.PassiveHealth.FailureThreshold {
		node.failures = 0
		node.unhealthyUntil = m.now().Add(group.cfg.PassiveHealth.Cooldown)
	}
	if group.cfg.Strategy == StrategyFillFirst && len(group.cfg.Nodes) > 0 {
		group.currentIndex = (idx + 1) % len(group.cfg.Nodes)
	}
}

func (m *Manager) lookupNode(groupName, nodeName string) (*groupState, *nodeState, int) {
	group, ok := m.groups[groupName]
	if !ok {
		return nil, nil, -1
	}
	for idx := range group.cfg.Nodes {
		if group.cfg.Nodes[idx].Name == nodeName {
			return group, &group.nodes[idx], idx
		}
	}
	return nil, nil, -1
}

func (g *groupState) selectRoundRobin(now time.Time, excluded map[string]bool) (int, bool) {
	for offset := 0; offset < len(g.cfg.Nodes); offset++ {
		idx := (g.nextIndex + offset) % len(g.cfg.Nodes)
		if g.selectable(idx, now, excluded) {
			return idx, true
		}
	}
	return 0, false
}

func (g *groupState) selectFillFirst(now time.Time, excluded map[string]bool) (int, bool) {
	for offset := 0; offset < len(g.cfg.Nodes); offset++ {
		idx := (g.currentIndex + offset) % len(g.cfg.Nodes)
		if g.selectable(idx, now, excluded) {
			return idx, true
		}
	}
	return 0, false
}

func (g *groupState) selectable(idx int, now time.Time, excluded map[string]bool) bool {
	node := g.cfg.Nodes[idx]
	if excluded != nil && excluded[node.Name] {
		return false
	}

	state := &g.nodes[idx]
	if !state.unhealthyUntil.IsZero() && !now.Before(state.unhealthyUntil) {
		state.unhealthyUntil = time.Time{}
		state.failures = 0
	}
	return state.unhealthyUntil.IsZero()
}

func normalizeGroup(group Group) (Group, error) {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" {
		return Group{}, errors.New("provider group name is required")
	}
	if group.Strategy == "" {
		group.Strategy = StrategyRoundRobin
	}
	if group.Strategy != StrategyRoundRobin && group.Strategy != StrategyFillFirst {
		return Group{}, fmt.Errorf("provider group %q has unsupported strategy %q", group.Name, group.Strategy)
	}
	if group.Timeout <= 0 {
		return Group{}, fmt.Errorf("provider group %q timeout must be greater than zero", group.Name)
	}
	if group.RetryCount < 0 {
		return Group{}, fmt.Errorf("provider group %q retry count cannot be negative", group.Name)
	}
	if len(group.Nodes) == 0 {
		return Group{}, fmt.Errorf("provider group %q requires at least one node", group.Name)
	}
	if group.PassiveHealth.FailureThreshold <= 0 {
		return Group{}, fmt.Errorf("provider group %q failure threshold must be greater than zero", group.Name)
	}
	if group.PassiveHealth.Cooldown <= 0 {
		return Group{}, fmt.Errorf("provider group %q cooldown must be greater than zero", group.Name)
	}

	if group.MaxNodeAttempts <= 0 || group.MaxNodeAttempts > len(group.Nodes) {
		group.MaxNodeAttempts = len(group.Nodes)
	}

	seen := make(map[string]bool, len(group.Nodes))
	normalizedNodes := make([]Node, 0, len(group.Nodes))
	for _, node := range group.Nodes {
		node.Name = strings.TrimSpace(node.Name)
		if node.Name == "" {
			return Group{}, fmt.Errorf("provider group %q has a node without a name", group.Name)
		}
		if seen[node.Name] {
			return Group{}, fmt.Errorf("provider group %q contains duplicate node %q", group.Name, node.Name)
		}
		seen[node.Name] = true

		node.BaseURL = strings.TrimSpace(node.BaseURL)
		if node.BaseURL == "" {
			return Group{}, fmt.Errorf("provider group %q node %q base URL is required", group.Name, node.Name)
		}
		node.Headers = cloneHeaders(node.Headers)
		normalizedNodes = append(normalizedNodes, node)
	}
	group.Nodes = normalizedNodes

	return group, nil
}

func cloneGroup(group Group) Group {
	cloned := group
	cloned.Nodes = make([]Node, len(group.Nodes))
	for idx, node := range group.Nodes {
		cloned.Nodes[idx] = cloneNode(node)
	}
	return cloned
}

func cloneNode(node Node) Node {
	cloned := node
	cloned.Headers = cloneHeaders(node.Headers)
	return cloned
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
