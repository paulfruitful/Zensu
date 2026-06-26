package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"zensu/internal/api"
	"zensu/internal/chrome"
	"zensu/internal/config"
	"zensu/internal/kwik"
	"zensu/internal/logger"
	"zensu/internal/server"
)

func main() {
	if err := logger.Init(); err != nil {
		fmt.Printf("Error initializing logger: %v\n", err)
	}
	defer logger.Close()

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("[ERROR] config error: %v\n", err)
		os.Exit(1)
	}

	needsSolve := cfg.UA == "" || cfg.CF == ""
	var client *api.Client

	if !needsSolve {
		var clientErr error
		client, clientErr = api.NewClient(cfg.UA, cfg.Cookies, cfg.Domain)
		if clientErr == nil {
			fmt.Println("  \033[32m[INFO]\033[0m Testing connection to domain...")
			if connErr := client.TestConnection(); connErr != nil {
				logger.Warnf("SERVER_STARTUP_CONN_FAIL", "Connection test failed: %v", connErr)
				fmt.Printf("  \033[33m[WARN]\033[0m Connection test failed: %v\n", connErr)
				needsSolve = true
			} else {
				fmt.Println("  \033[32m[SUCCESS]\033[0m Connection test passed! Credentials are valid.")
			}
		} else {
			needsSolve = true
		}
	}

	if needsSolve {
		if cfg.UA == "" || cfg.CF == "" {
			fmt.Println("  \033[33m[INFO]\033[0m Missing Cloudflare credentials (UA or cf_clearance).")
		} else {
			fmt.Println("  \033[33m[INFO]\033[0m Clearance cookies are expired or invalid.")
		}
		if err := refreshCredentials(cfg); err != nil {
			fmt.Printf("[ERROR] failed to resolve Cloudflare credentials: %v\n", err)
			os.Exit(1)
		}

		var clientErr error
		client, clientErr = api.NewClient(cfg.UA, cfg.Cookies, cfg.Domain)
		if clientErr != nil {
			fmt.Printf("[ERROR] failed to init client: %v\n", clientErr)
			os.Exit(1)
		}
	}

	extractor := kwik.NewExtractor(cfg.UA, cfg.Cookies)

	// Create router from modular internal package
	router := server.NewRouter(client, extractor, cfg)

	port := cfg.ServerPort
	if port <= 0 {
		port = 8080
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n  [INFO] Shutting down streaming server...")
		httpServer.Close()
		os.Exit(0)
	}()

	fmt.Println()
	fmt.Printf("  \033[1;36mZensu Streaming Server running at http://%s\033[0m\n", serverAddr)
	fmt.Println("  Press Ctrl+C to stop.")
	fmt.Println()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("[ERROR] server failed: %v\n", err)
		os.Exit(1)
	}
}

func refreshCredentials(cfg *config.Config) error {
	fmt.Println("  \033[33m[INFO]\033[0m Launching Chrome to solve Cloudflare challenge...")
	fmt.Println("         (Please click/solve any verification challenge if prompted)")
	credentials, err := chrome.FetchCredentials(cfg.Domain)
	if err != nil {
		return err
	}
	cfg.UA = credentials.UA
	cfg.CF = credentials.CF
	cfg.Cookies = "cf_clearance=" + credentials.CF
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("  \033[32m[SUCCESS]\033[0m Credentials fetched and saved successfully!")
	return nil
}
