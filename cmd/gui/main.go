package main

import (
	"flag"
	"log"

	appcore "zenhub/internal/client/app"
	clientconfig "zenhub/internal/client/config"
	"zenhub/internal/client/gui"
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
	log.Printf("zenhub tray listening on http://%s", status.ListenAddress)

	if err := gui.Run(runtime); err != nil {
		log.Fatalf("run tray: %v", err)
	}
}
