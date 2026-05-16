package controlgrpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"zenhub/internal/core/runtimeconfig"
	controlv1 "zenhub/internal/gen/controlv1"
	communityapi "zenhub/internal/server/community/api"
	communityauth "zenhub/internal/server/community/auth"
	communityrelay "zenhub/internal/server/community/relay"
	memorystore "zenhub/internal/server/community/storage/memory"
	communitysync "zenhub/internal/server/community/sync"
)

func TestCommunityControlGRPCLoginStatusPullAndProviders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	store := memorystore.NewStore()
	authService, err := communityauth.New("admin", "secret-pass", "0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("auth.New() error = %v", err)
	}
	syncService := communitysync.NewService(store)
	relayService := communityrelay.NewService(store, &http.Client{})

	snapshot := runtimeconfig.Snapshot{
		Routes: []runtimeconfig.Route{
			{
				Model:         "grpc-model",
				Mode:          "direct",
				ProviderGroup: "upstream",
				UpstreamModel: "upstream-model",
			},
		},
		ProviderGroups: []runtimeconfig.ProviderGroup{
			{
				Name:            "upstream",
				Strategy:        "round_robin",
				Timeout:         runtimeconfig.Duration{Duration: 2 * time.Second},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: runtimeconfig.PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         runtimeconfig.Duration{Duration: time.Second},
				},
				Nodes: []runtimeconfig.Node{
					{Name: "primary", BaseURL: upstream.URL},
				},
			},
		},
	}
	hash, err := communitysync.HashSnapshot(snapshot)
	if err != nil {
		t.Fatalf("HashSnapshot() error = %v", err)
	}
	if _, err := syncService.Push(context.Background(), communitysync.PushRequest{
		LocalModifiedAt: time.Now().UTC().UnixMilli(),
		LocalHash:       hash,
		Snapshot:        snapshot,
	}); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	httpHandler := communityapi.New(authService, syncService, relayService)
	grpcServer := NewServer(authService, syncService, relayService)
	server := httptest.NewServer(NewMixedHandler(httpHandler, grpcServer))
	t.Cleanup(server.Close)

	conn, err := grpc.DialContext(context.Background(), server.Listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	authClient := controlv1.NewAuthServiceClient(conn)
	controlClient := controlv1.NewCommunityControlServiceClient(conn)

	loginResp, err := authClient.Login(context.Background(), &controlv1.LoginRequest{
		Username: "admin",
		Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loginResp.GetAccessToken() == "" {
		t.Fatal("Login().AccessToken is empty")
	}

	authCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.GetAccessToken())

	statusResp, err := controlClient.Status(authCtx, &controlv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !statusResp.GetHasSnapshot() || statusResp.GetCloudHash() != hash {
		t.Fatalf("Status() = %#v, want snapshot hash %q", statusResp, hash)
	}

	pullResp, err := controlClient.Pull(authCtx, &controlv1.PullRequest{
		LastSyncAt:      statusResp.GetCloudUpdatedAt(),
		LocalModifiedAt: statusResp.GetCloudUpdatedAt(),
		LocalHash:       hash,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if pullResp.GetStatus() != communitysync.StatusUpToDate {
		t.Fatalf("Pull().Status = %q, want up_to_date", pullResp.GetStatus())
	}

	providersResp, err := controlClient.GetProviders(authCtx, &controlv1.GetProvidersRequest{})
	if err != nil {
		t.Fatalf("GetProviders() error = %v", err)
	}
	if providersResp.GetCloudHash() != hash || len(providersResp.GetProviderGroups()) != 1 {
		t.Fatalf("GetProviders() = %#v", providersResp)
	}
	if providersResp.GetProviderGroups()[0].GetName() != "upstream" {
		t.Fatalf("GetProviders().ProviderGroups[0].Name = %q, want upstream", providersResp.GetProviderGroups()[0].GetName())
	}
}
