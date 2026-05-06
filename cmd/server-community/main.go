package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	appcore "zenhub/internal/server/community/app"
	serverconfig "zenhub/internal/server/community/config"
)

func main() {
	var bootstrapConfig string
	var listen string

	flag.StringVar(&bootstrapConfig, "bootstrap-config", "", "path to a client-style config file used to seed the initial server snapshot")
	flag.StringVar(&listen, "listen", "", "override the listen address from ZENHUB_SERVER_LISTEN")
	flag.Parse()

	cfg, err := serverconfig.LoadEnv()
	if err != nil {
		log.Fatalf("load server config: %v", err)
	}
	if bootstrapConfig != "" {
		cfg.BootstrapConfigPath = bootstrapConfig
	}
	if listen != "" {
		cfg.Listen = listen
	}

	runtime, err := appcore.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("build community server runtime: %v", err)
	}

	if err := runtime.Start(); err != nil {
		log.Fatalf("start community server runtime: %v", err)
	}

	status := runtime.Status()
	log.Printf("zenhub community server listening on http://%s", status.ListenAddress)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	<-signals

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runtime.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown community server runtime: %v", err)
	}
}
