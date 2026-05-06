package sync

import (
	"context"
	"testing"
	"time"

	"zenhub/internal/core/runtimeconfig"
	memorystore "zenhub/internal/server/community/storage/memory"
)

func TestServicePushStatusAndPullRoundTrip(t *testing.T) {
	store := memorystore.NewStore()
	service := NewService(store)

	now := time.Unix(1_700_000_000, 0).UTC()
	service.now = func() time.Time { return now }

	snapshot := testSnapshot("relay-model", "https://provider.example.com")
	hash, err := HashSnapshot(snapshot)
	if err != nil {
		t.Fatalf("HashSnapshot() error = %v", err)
	}

	pushResponse, err := service.Push(context.Background(), PushRequest{
		LocalHash:       hash,
		LocalModifiedAt: now.UnixMilli(),
		Snapshot:        snapshot,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if pushResponse.Status != StatusApplied {
		t.Fatalf("Push().Status = %q, want %q", pushResponse.Status, StatusApplied)
	}

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.HasSnapshot || status.CloudHash != hash {
		t.Fatalf("Status() = %#v", status)
	}

	pullResponse, err := service.Pull(context.Background(), PullRequest{
		LastSyncAt:      pushResponse.CloudUpdatedAt,
		LocalModifiedAt: pushResponse.CloudUpdatedAt,
		LocalHash:       hash,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if pullResponse.Status != StatusUpToDate {
		t.Fatalf("Pull().Status = %q, want %q", pullResponse.Status, StatusUpToDate)
	}
}

func TestServiceDetectsPullAndPushConflicts(t *testing.T) {
	store := memorystore.NewStore()
	service := NewService(store)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	service.now = func() time.Time { return baseTime }

	initial := testSnapshot("relay-model", "https://provider-a.example.com")
	initialRecord, err := service.Bootstrap(context.Background(), initial)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	cloudTime := baseTime.Add(10 * time.Minute)
	service.now = func() time.Time { return cloudTime }

	cloudSnapshot := testSnapshot("relay-model", "https://provider-b.example.com")
	cloudHash, err := HashSnapshot(cloudSnapshot)
	if err != nil {
		t.Fatalf("HashSnapshot(cloudSnapshot) error = %v", err)
	}
	if _, err := service.saveSnapshot(context.Background(), cloudSnapshot, cloudHash); err != nil {
		t.Fatalf("saveSnapshot() error = %v", err)
	}

	localSnapshot := testSnapshot("relay-model", "https://provider-c.example.com")
	localHash, err := HashSnapshot(localSnapshot)
	if err != nil {
		t.Fatalf("HashSnapshot(localSnapshot) error = %v", err)
	}

	pullResponse, err := service.Pull(context.Background(), PullRequest{
		LastSyncAt:      initialRecord.UpdatedAt.UnixMilli(),
		LocalModifiedAt: cloudTime.Add(5 * time.Minute).UnixMilli(),
		LocalHash:       localHash,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if pullResponse.Status != StatusConflict || pullResponse.Snapshot == nil {
		t.Fatalf("Pull() = %#v, want conflict with snapshot", pullResponse)
	}

	pushResponse, err := service.Push(context.Background(), PushRequest{
		LastSyncAt:      initialRecord.UpdatedAt.UnixMilli(),
		LocalModifiedAt: cloudTime.Add(5 * time.Minute).UnixMilli(),
		LocalHash:       localHash,
		Snapshot:        localSnapshot,
	})
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if pushResponse.Status != StatusConflict || pushResponse.Snapshot == nil {
		t.Fatalf("Push() = %#v, want conflict with cloud snapshot", pushResponse)
	}
}

func testSnapshot(model, baseURL string) runtimeconfig.Snapshot {
	return runtimeconfig.Snapshot{
		Routes: []runtimeconfig.Route{
			{
				Model:         model,
				Mode:          "relay",
				ProviderGroup: "relay",
				UpstreamModel: "upstream-model",
			},
		},
		ProviderGroups: []runtimeconfig.ProviderGroup{
			{
				Name:            "relay",
				Strategy:        "round_robin",
				Timeout:         runtimeconfig.Duration{Duration: 2 * time.Second},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: runtimeconfig.PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         runtimeconfig.Duration{Duration: time.Second},
				},
				Nodes: []runtimeconfig.Node{
					{
						Name:    "primary",
						BaseURL: baseURL,
						Headers: map[string]string{"X-Test": "1"},
					},
				},
			},
		},
	}
}
