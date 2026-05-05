package main

import (
	"flag"
	"log"

	appcore "zenhub/internal/client/app"
	"zenhub/internal/client/gui"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to the client config file")
	flag.Parse()

	if configPath == "" {
		log.Fatal("-config is required")
	}

	runtime, err := appcore.NewRuntime(configPath)
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}

	if err := runtime.Start(); err != nil {
		log.Fatalf("start runtime: %v", err)
	}

	status := runtime.Status()
	log.Printf("zenhub gui listening on http://%s", status.ListenAddress)

	if err := gui.Run(runtime); err != nil {
		log.Fatalf("run gui: %v", err)
	}
}
