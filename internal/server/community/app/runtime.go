package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"zenhub/internal/core/runtimeconfig"
	"zenhub/internal/server/community/api"
	"zenhub/internal/server/community/auth"
	"zenhub/internal/server/community/config"
	"zenhub/internal/server/community/relay"
	communitystorage "zenhub/internal/server/community/storage"
	mysqlstorage "zenhub/internal/server/community/storage/mysql"
	communitysync "zenhub/internal/server/community/sync"
)

const readHeaderTimeout = 5 * time.Second

type Runtime struct {
	mu            sync.RWMutex
	config        config.Config
	auth          *auth.Service
	sync          *communitysync.Service
	relay         *relay.Service
	store         communitystorage.Store
	httpServer    *http.Server
	listener      net.Listener
	listenAddress string
	running       bool
	serveErr      error
	closeOnce     sync.Once
	closeErr      error
}

type StatusView struct {
	Running         bool
	ListenAddress   string
	HasSnapshot     bool
	SnapshotVersion int64
	SnapshotHash    string
	LastPullAt      time.Time
	LastPushAt      time.Time
	ServiceError    string
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := mysqlstorage.Open(cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = store.Close()
		}
	}()

	if err := store.EnsureSchema(context.Background()); err != nil {
		return nil, err
	}

	authService, err := auth.New(cfg.AdminUsername, cfg.AdminPassword, cfg.TokenSecret, cfg.TokenTTL)
	if err != nil {
		return nil, err
	}

	syncService := communitysync.NewService(store)
	if strings.TrimSpace(cfg.BootstrapConfigPath) != "" {
		bootstrapFile, err := runtimeconfig.LoadFile(cfg.BootstrapConfigPath)
		if err != nil {
			return nil, err
		}
		if _, err := syncService.Bootstrap(context.Background(), bootstrapFile.Snapshot()); err != nil {
			return nil, err
		}
	}

	relayService := relay.NewService(store, &http.Client{})
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(authService, syncService, relayService),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	cleanup = false
	return &Runtime{
		config:        cfg,
		auth:          authService,
		sync:          syncService,
		relay:         relayService,
		store:         store,
		httpServer:    httpServer,
		listenAddress: cfg.Listen,
	}, nil
}

func (r *Runtime) Start() error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.serveErr = nil
	addr := r.httpServer.Addr
	r.mu.Unlock()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.listener = listener
	r.listenAddress = listener.Addr().String()
	r.running = true
	serverInstance := r.httpServer
	r.mu.Unlock()

	go func() {
		err := serverInstance.Serve(listener)
		r.mu.Lock()
		defer r.mu.Unlock()

		r.running = false
		r.listener = nil
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.serveErr = err
			return
		}
		r.serveErr = nil
	}()

	return nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.RLock()
	serverInstance := r.httpServer
	listener := r.listener
	r.mu.RUnlock()

	if listener == nil {
		return r.closeStore()
	}

	shutdownErr := serverInstance.Shutdown(ctx)
	closeErr := r.closeStore()
	if shutdownErr != nil {
		return shutdownErr
	}
	return closeErr
}

func (r *Runtime) Status() StatusView {
	r.mu.RLock()
	status := StatusView{
		Running:       r.running,
		ListenAddress: r.listenAddress,
	}
	if r.serveErr != nil {
		status.ServiceError = r.serveErr.Error()
	}
	r.mu.RUnlock()

	syncStatus, err := r.sync.Status(context.Background())
	if err != nil {
		if status.ServiceError == "" {
			status.ServiceError = err.Error()
		}
		return status
	}

	status.HasSnapshot = syncStatus.HasSnapshot
	status.SnapshotVersion = syncStatus.Version
	status.SnapshotHash = syncStatus.CloudHash
	status.LastPullAt = millisToTime(syncStatus.LastPullAt)
	status.LastPushAt = millisToTime(syncStatus.LastPushAt)
	return status
}

func (r *Runtime) closeStore() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.store.Close()
	})
	return r.closeErr
}

func millisToTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
