package router

import (
	"errors"
	"reflect"
	"testing"

	"zenhub/internal/canonical"
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
