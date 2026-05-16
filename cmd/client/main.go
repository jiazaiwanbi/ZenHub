package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	appcore "zenhub/internal/client/app"
	clientconfig "zenhub/internal/client/config"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to the client config file")
	flag.Parse()

	paths, created, err := clientconfig.ResolveOrCreateClientConfig(configPath)
	if err != nil {
		log.Fatalf("resolve client config: %v", err)
	}
	configPath = paths.ConfigPath
	if created {
		log.Printf("created starter client config at %s", configPath)
	}
	log.Printf("using client config %s", configPath)

	runtime, err := appcore.NewRuntime(configPath)
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}

	if err := runtime.Start(); err != nil {
		log.Fatalf("start runtime: %v", err)
	}

	status := runtime.Status()
	log.Printf("zenhub client listening on http://%s", status.ListenAddress)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	<-signals

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runtime.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown runtime: %v", err)
	}
}
