package router

import (
	"errors"
	"reflect"
	"testing"

	"zenhub/internal/core/canonical"
)

func TestRouterSelectAndModels(t *testing.T) {
	instance, err := New([]Rule{
		{Model: "z-model", ProviderGroup: "primary"},
		{Model: "a-model", Mode: RouteModeRelay, ProviderGroup: "relay", UpstreamModel: "relay-model"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	models := instance.Models()
	if want := []string{"a-model", "z-model"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("Models() = %v, want %v", models, want)
	}

	decision, err := instance.Select(canonical.ChatRequest{Model: "a-model"})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if decision.Mode != RouteModeRelay {
		t.Fatalf("Mode = %q, want %q", decision.Mode, RouteModeRelay)
	}
	if decision.UpstreamModel != "relay-model" {
		t.Fatalf("UpstreamModel = %q, want relay-model", decision.UpstreamModel)
	}

	_, err = instance.Select(canonical.ChatRequest{Model: "missing"})
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("Select() error = %v, want ErrNoRoute", err)
	}
}

func TestRouterTreatsDuplicateModelsAsPool(t *testing.T) {
	instance, err := New([]Rule{
		{Model: "sonnet", ProviderGroup: "anthropic", UpstreamModel: "claude-3-7-sonnet"},
		{Model: "sonnet", ProviderGroup: "openrouter", UpstreamModel: "anthropic/claude-3.7-sonnet"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	models := instance.Models()
	if want := []string{"sonnet"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("Models() = %v, want %v", models, want)
	}

	first, err := instance.Select(canonical.ChatRequest{Model: "sonnet"})
	if err != nil {
		t.Fatalf("first Select() error = %v", err)
	}
	second, err := instance.Select(canonical.ChatRequest{Model: "sonnet"})
	if err != nil {
		t.Fatalf("second Select() error = %v", err)
	}

	if first.ProviderGroup != "anthropic" || second.ProviderGroup != "openrouter" {
		t.Fatalf("pool order = %q then %q, want anthropic then openrouter", first.ProviderGroup, second.ProviderGroup)
	}

	third, err := instance.SelectWithExclusions(canonical.ChatRequest{Model: "sonnet"}, map[string]bool{
		first.Key: true,
	})
	if err != nil {
		t.Fatalf("SelectWithExclusions() error = %v", err)
	}
	if third.ProviderGroup != "openrouter" {
		t.Fatalf("SelectWithExclusions().ProviderGroup = %q, want openrouter", third.ProviderGroup)
	}
}
