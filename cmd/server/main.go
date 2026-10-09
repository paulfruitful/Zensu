package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"zensu/internal/api"
	"zensu/internal/browser"
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

		fmt.Println("  \033[32m[INFO]\033[0m Verifying credentials connection...")
		if connErr := client.TestConnection(); connErr != nil {
			logger.Errorf("SERVER_STARTUP_CONN_FAIL", "Connection test failed after refresh: %v", connErr)
			fmt.Printf("  \033[31m[ERROR]\033[0m Connection verification failed: %v\n", connErr)
			os.Exit(1)
		}
		fmt.Println("  \033[32m[SUCCESS]\033[0m Connection test passed! Credentials are valid.")
	}

	extractor := kwik.NewExtractor(cfg.UA, cfg.Cookies)

	// Callback to launch browser, refresh Cloudflare cookies, and reinitialize clients
	refreshFunc := func() (*api.Client, *kwik.Extractor, error) {
		if err := refreshCredentials(cfg); err != nil {
			return nil, nil, err
		}
		newClient, err := api.NewClient(cfg.UA, cfg.Cookies, cfg.Domain)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to init client: %w", err)
		}
		newExtractor := kwik.NewExtractor(cfg.UA, cfg.Cookies)
		return newClient, newExtractor, nil
	}

	srv := server.NewServer(client, extractor, cfg, refreshFunc)
	router := srv.Router()

	// Background monitor to proactively detect expired cookies and launch browser
	stopMonitor := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-ticker.C:
				currentClient := srv.Client()
				if currentClient == nil {
					continue
				}
				if connErr := currentClient.TestConnection(); connErr != nil && server.IsExpiredError(connErr) {
					logger.Warnf("SERVER_COOKIE_EXPIRED", "Background check detected expired cookie (%v). Launching browser to refresh...", connErr)
					fmt.Println("\n  \033[33m[WARN]\033[0m Background check: Cloudflare clearance cookie expired or invalid.")
					if err := srv.RefreshCredentials(); err != nil {
						logger.Errorf("SERVER_COOKIE_REFRESH_ERR", "Failed to refresh cookie in background: %v", err)
						fmt.Printf("  \033[31m[ERROR]\033[0m Failed to refresh credentials: %v\n", err)
					} else {
						logger.Infof("SERVER_COOKIE_REFRESH_OK", "Successfully refreshed credentials and reconnected")
						fmt.Println("  \033[32m[SUCCESS]\033[0m Credentials refreshed successfully! Streaming server ready.")
					}
				}
			}
		}
	}()

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
		close(stopMonitor)
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
	fmt.Println("  \033[33m[INFO]\033[0m Launching browser to solve Cloudflare challenge...")
	fmt.Println("         (Please click/solve any verification challenge if prompted)")
	credentials, err := browser.FetchCredentials(cfg.Domain, cfg.Browser, cfg.BrowserPath, cfg.CF)
	if err != nil {
		return err
	}
	cfg.UA = credentials.UA
	cfg.CF = credentials.CF
	if credentials.Cookies != "" {
		cfg.Cookies = credentials.Cookies
	} else {
		cfg.Cookies = "cf_clearance=" + credentials.CF
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("  \033[32m[SUCCESS]\033[0m Credentials fetched and saved successfully!")
	return nil
}
