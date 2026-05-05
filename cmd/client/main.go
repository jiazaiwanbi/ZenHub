package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"zenhub/internal/balancer"
	"zenhub/internal/config"
	"zenhub/internal/executor"
	"zenhub/internal/observability"
	"zenhub/internal/proxy"
	"zenhub/internal/router"
	"zenhub/internal/server"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to the client config file")
	flag.Parse()

	if configPath == "" {
		log.Fatal("-config is required")
	}

	runtimeConfig, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	routerInstance, err := router.New(runtimeConfig.Routes)
	if err != nil {
		log.Fatalf("build router: %v", err)
	}

	balancerInstance, err := balancer.New(runtimeConfig.ProviderGroups)
	if err != nil {
		log.Fatalf("build balancer: %v", err)
	}

	service, err := proxy.New(
		routerInstance,
		balancerInstance,
		executor.NewDirect(&http.Client{}),
		observability.NewRecorder(runtimeConfig.ObservabilityLimit),
	)
	if err != nil {
		log.Fatalf("build service: %v", err)
	}

	httpServer := &http.Server{
		Addr:              runtimeConfig.Listen,
		Handler:           server.New(service),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("zenhub client listening on http://%s", runtimeConfig.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
