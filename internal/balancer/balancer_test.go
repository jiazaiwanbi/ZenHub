package balancer

import (
	"testing"
	"time"
)

func TestRoundRobinRotatesHealthyNodes(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	manager, err := NewWithClock([]Group{
		testGroup(StrategyRoundRobin),
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("NewWithClock() error = %v", err)
	}

	first, _ := manager.Select("group", nil)
	second, _ := manager.Select("group", nil)
	third, _ := manager.Select("group", nil)

	if first.Node.Name != "node-a" || second.Node.Name != "node-b" || third.Node.Name != "node-a" {
		t.Fatalf("round robin order = %q, %q, %q", first.Node.Name, second.Node.Name, third.Node.Name)
	}
}

func TestFillFirstSticksUntilFailure(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	manager, err := NewWithClock([]Group{
		testGroup(StrategyFillFirst),
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("NewWithClock() error = %v", err)
	}

	first, _ := manager.Select("group", nil)
	manager.ReportSuccess("group", first.Node.Name)
	second, _ := manager.Select("group", nil)
	manager.ReportFailure("group", second.Node.Name)
	third, _ := manager.Select("group", nil)

	if first.Node.Name != "node-a" || second.Node.Name != "node-a" {
		t.Fatalf("fill_first should stick to node-a, got %q then %q", first.Node.Name, second.Node.Name)
	}
	if third.Node.Name != "node-b" {
		t.Fatalf("after failure Select() = %q, want node-b", third.Node.Name)
	}
}

func TestPassiveHealthCooldownRecovery(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	manager, err := NewWithClock([]Group{
		testGroup(StrategyRoundRobin),
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("NewWithClock() error = %v", err)
	}

	first, _ := manager.Select("group", nil)
	manager.ReportFailure("group", first.Node.Name)

	second, _ := manager.Select("group", nil)
	if second.Node.Name != "node-b" {
		t.Fatalf("Select() after unhealthy node = %q, want node-b", second.Node.Name)
	}

	now = now.Add(2 * time.Minute)
	third, _ := manager.Select("group", nil)
	if third.Node.Name != "node-a" {
		t.Fatalf("Select() after cooldown = %q, want node-a", third.Node.Name)
	}
}

func testGroup(strategy Strategy) Group {
	return Group{
		Name:            "group",
		Strategy:        strategy,
		Timeout:         time.Second,
		RetryCount:      0,
		MaxNodeAttempts: 2,
		PassiveHealth: PassiveHealth{
			FailureThreshold: 1,
			Cooldown:         time.Minute,
		},
		Nodes: []Node{
			{Name: "node-a", BaseURL: "http://node-a"},
			{Name: "node-b", BaseURL: "http://node-b"},
		},
	}
}
