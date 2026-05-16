package controlgrpc

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"zenhub/internal/controlplane"
	"zenhub/internal/core/runtimeconfig"
	controlv1 "zenhub/internal/gen/controlv1"
	communityauth "zenhub/internal/server/community/auth"
	communityrelay "zenhub/internal/server/community/relay"
	communitystorage "zenhub/internal/server/community/storage"
	communitysync "zenhub/internal/server/community/sync"
)

type authServer struct {
	controlv1.UnimplementedAuthServiceServer
	auth *communityauth.Service
}

type controlServer struct {
	controlv1.UnimplementedCommunityControlServiceServer
	sync  *communitysync.Service
	relay *communityrelay.Service
}

func NewServer(
	auth *communityauth.Service,
	sync *communitysync.Service,
	relay *communityrelay.Service,
) *grpc.Server {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(authInterceptor(auth)),
	)
	controlv1.RegisterAuthServiceServer(server, &authServer{auth: auth})
	controlv1.RegisterCommunityControlServiceServer(server, &controlServer{
		sync:  sync,
		relay: relay,
	})
	return server
}

func NewMixedHandler(httpHandler http.Handler, grpcServer *grpc.Server) http.Handler {
	mixed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.Contains(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
			return
		}
		httpHandler.ServeHTTP(w, r)
	})
	return h2c.NewHandler(mixed, &http2.Server{})
}

func (s *authServer) Login(ctx context.Context, req *controlv1.LoginRequest) (*controlv1.LoginResponse, error) {
	session, err := s.auth.Login(req.GetUsername(), req.GetPassword())
	if err != nil {
		if errors.Is(err, communityauth.ErrInvalidCredentials) {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &controlv1.LoginResponse{
		AccessToken: session.AccessToken,
		TokenType:   session.TokenType,
		ExpiresIn:   session.ExpiresIn,
		ExpiresAt:   session.ExpiresAt,
		Username:    session.Username,
	}, nil
}

func (s *controlServer) Status(ctx context.Context, _ *controlv1.StatusRequest) (*controlv1.StatusResponse, error) {
	value, err := s.sync.Status(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return &controlv1.StatusResponse{
		HasSnapshot:    value.HasSnapshot,
		Version:        value.Version,
		CloudUpdatedAt: value.CloudUpdatedAt,
		CloudHash:      value.CloudHash,
		LastPullAt:     value.LastPullAt,
		LastPushAt:     value.LastPushAt,
	}, nil
}

func (s *controlServer) Pull(ctx context.Context, req *controlv1.PullRequest) (*controlv1.PullResponse, error) {
	value, err := s.sync.Pull(ctx, communitysync.PullRequest{
		LastSyncAt:      req.GetLastSyncAt(),
		LocalModifiedAt: req.GetLocalModifiedAt(),
		LocalHash:       req.GetLocalHash(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &controlv1.PullResponse{
		Status:         value.Status,
		Version:        value.Version,
		CloudUpdatedAt: value.CloudUpdatedAt,
		CloudHash:      value.CloudHash,
		Snapshot:       toProtoSnapshot(value.Snapshot),
	}, nil
}

func (s *controlServer) Push(ctx context.Context, req *controlv1.PushRequest) (*controlv1.PushResponse, error) {
	snapshot, err := controlplane.FromProtoSnapshot(req.GetSnapshot())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	value, err := s.sync.Push(ctx, communitysync.PushRequest{
		LastSyncAt:      req.GetLastSyncAt(),
		LocalModifiedAt: req.GetLocalModifiedAt(),
		LocalHash:       req.GetLocalHash(),
		Snapshot:        snapshot,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &controlv1.PushResponse{
		Status:         value.Status,
		Version:        value.Version,
		CloudUpdatedAt: value.CloudUpdatedAt,
		CloudHash:      value.CloudHash,
		Snapshot:       toProtoSnapshot(value.Snapshot),
	}, nil
}

func (s *controlServer) GetProviders(ctx context.Context, _ *controlv1.GetProvidersRequest) (*controlv1.GetProvidersResponse, error) {
	value, err := s.relay.ProviderCatalog(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return &controlv1.GetProvidersResponse{
		Version:        value.Version,
		CloudUpdatedAt: value.CloudUpdatedAt,
		CloudHash:      value.CloudHash,
		ProviderGroups: controlplane.ToProtoProviderGroups(value.ProviderGroups),
	}, nil
}

func authInterceptor(auth *communityauth.Service) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if info.FullMethod == controlv1.AuthService_Login_FullMethodName {
			return handler(ctx, req)
		}

		header, err := authorizationHeader(ctx)
		if err != nil {
			return nil, err
		}
		if _, err := auth.Verify(header); err != nil {
			return nil, mapError(err)
		}
		return handler(ctx, req)
	}
}

func authorizationHeader(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, communityauth.ErrInvalidToken.Error())
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, communityauth.ErrInvalidToken.Error())
	}

	header := strings.TrimSpace(values[0])
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", status.Error(codes.Unauthenticated, communityauth.ErrInvalidToken.Error())
	}
	return strings.TrimSpace(header[len("Bearer "):]), nil
}

func mapError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, communityauth.ErrInvalidCredentials),
		errors.Is(err, communityauth.ErrInvalidToken),
		errors.Is(err, communityauth.ErrExpiredToken):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, communitystorage.ErrSnapshotNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		if grpcStatus, ok := status.FromError(err); ok {
			return grpcStatus.Err()
		}
		if strings.Contains(err.Error(), "validate") || strings.Contains(err.Error(), "local_hash") {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		return status.Error(codes.Internal, err.Error())
	}
}

func derefSnapshot(snapshot *runtimeconfig.Snapshot) runtimeconfig.Snapshot {
	if snapshot == nil {
		return runtimeconfig.Snapshot{}
	}
	return *snapshot
}

func toProtoSnapshot(snapshot *runtimeconfig.Snapshot) *controlv1.Snapshot {
	if snapshot == nil {
		return nil
	}
	return controlplane.ToProtoSnapshot(*snapshot)
}
