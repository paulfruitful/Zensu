package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"zensu/internal/api"
	"zensu/internal/browser"
	"zensu/internal/kwik"
	"zensu/internal/logger"
)

type portalStatusResponse struct {
	Authenticated bool   `json:"authenticated"`
	Domain        string `json:"domain"`
	CFPreview     string `json:"cf_preview"`
	UAPreview     string `json:"ua_preview"`
	HasCookies    bool   `json:"has_cookies"`
	StatusMessage string `json:"status_message"`
}

type linkSubmitRequest struct {
	URL string `json:"url"`
}

type cookieSubmitRequest struct {
	CF      string `json:"cf"`
	UA      string `json:"ua"`
	Cookies string `json:"cookies"`
}

type actionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

func handlePortal(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(portalHTML))
	}
}

func handlePortalStatus(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.Config()
		client := s.Client()

		domain := "https://animepahe.pw"
		cf := ""
		ua := ""
		hasCookies := false

		if cfg != nil {
			if cfg.Domain != "" {
				domain = cfg.Domain
			}
			cf = cfg.CF
			ua = cfg.UA
			hasCookies = cfg.Cookies != ""
		}

		cfPreview := ""
		if len(cf) > 16 {
			cfPreview = cf[:8] + "..." + cf[len(cf)-6:]
		} else if cf != "" {
			cfPreview = cf
		}

		uaPreview := ""
		if len(ua) > 40 {
			uaPreview = ua[:37] + "..."
		} else if ua != "" {
			uaPreview = ua
		}

		authenticated := false
		statusMsg := "Not authenticated. Cloudflare clearance required."

		if client != nil {
			if err := client.TestConnection(); err == nil {
				authenticated = true
				statusMsg = "Connected and active. AnimePahe is responding 200 OK."
			} else {
				statusMsg = fmt.Sprintf("Authentication failed: %v", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(portalStatusResponse{
			Authenticated: authenticated,
			Domain:        domain,
			CFPreview:     cfPreview,
			UAPreview:     uaPreview,
			HasCookies:    hasCookies,
			StatusMessage: statusMsg,
		})
	}
}

func handlePortalSubmitLink(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req linkSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "invalid JSON payload",
			})
			return
		}

		rawURL := strings.TrimSpace(req.URL)
		if rawURL == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "URL cannot be empty",
			})
			return
		}

		if !strings.Contains(strings.ToLower(rawURL), "animepahe") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "URL must be an AnimePahe link (e.g. https://animepahe.pw/?__cf_chl_tk=...)",
			})
			return
		}

		logger.Infof("PORTAL_SUBMIT_LINK", "User submitted link to portal: %s", rawURL)

		cfg := s.Config()
		browserType := "auto"
		browserPath := ""
		oldCookie := ""
		domain := "https://animepahe.pw"

		if cfg != nil {
			if cfg.Browser != "" {
				browserType = cfg.Browser
			}
			browserPath = cfg.BrowserPath
			oldCookie = cfg.CF
			if cfg.Domain != "" {
				domain = cfg.Domain
			}
		}

		// Use browser to resolve the link and poll clearance
		creds, err := browser.FetchCredentialsFromURL(rawURL, browserType, browserPath, oldCookie)
		if err != nil {
			logger.Errorf("PORTAL_LINK_EXTRACT_ERR", "Failed to extract credentials from URL: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to extract clearance: %v", err),
			})
			return
		}

		// Initialize client and verify connection
		newClient, clientErr := api.NewClient(creds.UA, creds.Cookies, domain)
		if clientErr != nil {
			logger.Errorf("PORTAL_CLIENT_INIT_ERR", "Failed to init API client: %v", clientErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to initialize API client: %v", clientErr),
			})
			return
		}

		if connErr := newClient.TestConnection(); connErr != nil {
			logger.Warnf("PORTAL_CONN_TEST_FAIL", "Connection test failed with extracted cookies: %v", connErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Cookies extracted, but AnimePahe rejected connection (%v). If Cloudflare blocked the VPS IP, please use the Direct Cookie tab.", connErr),
			})
			return
		}

		// Update config and server
		if cfg != nil {
			cfg.UA = creds.UA
			cfg.CF = creds.CF
			cfg.Cookies = creds.Cookies
			if err := cfg.Save(); err != nil {
				logger.Warnf("PORTAL_CFG_SAVE_FAIL", "Failed to persist config: %v", err)
			}
		}

		newExtractor := kwik.NewExtractor(creds.UA, creds.Cookies)
		s.SetCredentials(newClient, newExtractor)

		logger.Infof("PORTAL_LINK_SUCCESS", "Successfully extracted credentials from link and reconnected!")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(actionResponse{
			Success: true,
			Message: "Successfully authenticated! Credentials extracted from link and verified with AnimePahe.",
		})
	}
}

func handlePortalSubmitCookies(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req cookieSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "invalid JSON payload",
			})
			return
		}

		cf := strings.TrimSpace(req.CF)
		ua := strings.TrimSpace(req.UA)
		cookies := strings.TrimSpace(req.Cookies)

		// Auto-populate User-Agent from incoming HTTP request if user didn't specify one
		if ua == "" {
			ua = r.UserAgent()
		}
		if ua == "" {
			ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
		}

		// Parse cf_clearance out of full cookie string if cf field is blank
		if cf == "" && strings.Contains(cookies, "cf_clearance=") {
			parts := strings.Split(cookies, ";")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "cf_clearance=") {
					cf = strings.TrimPrefix(p, "cf_clearance=")
					break
				}
			}
		}

		// Construct full cookies string if only cf is provided
		if cookies == "" && cf != "" {
			cookies = "cf_clearance=" + cf
		} else if cookies != "" && cf != "" && !strings.Contains(cookies, "cf_clearance=") {
			cookies = "cf_clearance=" + cf + "; " + cookies
		}

		if cf == "" && !strings.Contains(cookies, "cf_clearance=") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "cf_clearance value or a cookie string containing cf_clearance is required",
			})
			return
		}

		cfg := s.Config()
		domain := "https://animepahe.pw"
		if cfg != nil && cfg.Domain != "" {
			domain = cfg.Domain
		}

		// Test connection
		newClient, clientErr := api.NewClient(ua, cookies, domain)
		if clientErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to initialize API client: %v", clientErr),
			})
			return
		}

		if connErr := newClient.TestConnection(); connErr != nil {
			logger.Warnf("PORTAL_COOKIE_TEST_FAIL", "Connection test failed with submitted cookies: %v", connErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Connection test failed: %v. Please make sure the User-Agent matches the browser where the cookie was extracted.", connErr),
			})
			return
		}

		// Update config
		if cfg != nil {
			cfg.UA = ua
			cfg.CF = cf
			cfg.Cookies = cookies
			if err := cfg.Save(); err != nil {
				logger.Warnf("PORTAL_CFG_SAVE_FAIL", "Failed to persist config: %v", err)
			}
		}

		newExtractor := kwik.NewExtractor(ua, cookies)
		s.SetCredentials(newClient, newExtractor)

		logger.Infof("PORTAL_COOKIE_SUCCESS", "Successfully updated server credentials via direct cookie submission!")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(actionResponse{
			Success: true,
			Message: "Connection verified! Credentials successfully saved and streaming server is ready.",
		})
	}
}

func handlePortalTest(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := s.Client()
		if client == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   "No active client initialized. Please submit credentials first.",
			})
			return
		}

		if err := client.TestConnection(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Connection test failed: %v", err),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(actionResponse{
			Success: true,
			Message: "AnimePahe connection healthy! Status 200 OK.",
		})
	}
}

type triggerBrowserRequest struct {
	Browser string `json:"browser"`
}

func handlePortalTriggerBrowser(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req triggerBrowserRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		chosenBrowser := strings.ToLower(strings.TrimSpace(req.Browser))
		if chosenBrowser != "" {
			cfg := s.Config()
			if cfg != nil {
				cfg.Browser = chosenBrowser
				_ = cfg.Save()
			}
		}

		logger.Infof("PORTAL_TRIGGER_BROWSER", "User triggered browser Cloudflare verification (engine: %s)", chosenBrowser)
		err := s.RefreshCredentials()
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			logger.Errorf("PORTAL_BROWSER_ERR", "Browser verification failed: %v", err)
			json.NewEncoder(w).Encode(actionResponse{
				Success: false,
				Error:   fmt.Sprintf("Browser verification failed: %v", err),
			})
			return
		}

		logger.Infof("PORTAL_BROWSER_OK", "Browser verification succeeded and credentials updated")
		json.NewEncoder(w).Encode(actionResponse{
			Success: true,
			Message: "Browser verification completed successfully! Cloudflare credentials updated and server is connected.",
		})
	}
}

const portalHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
  <title>Zensu &bull; Cloudflare Clearance Portal</title>
  <style>
    :root {
      --bg: #090d16;
      --card-bg: rgba(18, 24, 38, 0.85);
      --card-border: rgba(255, 255, 255, 0.08);
      --primary: #6366f1;
      --primary-hover: #4f46e5;
      --primary-glow: rgba(99, 102, 241, 0.35);
      --success: #10b981;
      --success-glow: rgba(16, 185, 129, 0.3);
      --warning: #f59e0b;
      --error: #ef4444;
      --text-main: #f8fafc;
      --text-muted: #94a3b8;
      --input-bg: rgba(11, 15, 25, 0.9);
    }
    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    }
    body {
      background-color: var(--bg);
      color: var(--text-main);
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      padding: 24px 16px 48px;
      background-image: 
        radial-gradient(circle at 15% 15%, rgba(99, 102, 241, 0.12) 0%, transparent 40%),
        radial-gradient(circle at 85% 85%, rgba(16, 185, 129, 0.08) 0%, transparent 40%);
    }
    .container {
      width: 100%;
      max-width: 640px;
      display: flex;
      flex-direction: column;
      gap: 20px;
    }
    .header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      flex-wrap: wrap;
      gap: 12px;
      padding: 4px 0;
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 12px;
    }
    .logo-icon {
      width: 40px;
      height: 40px;
      border-radius: 10px;
      background: linear-gradient(135deg, #6366f1, #8b5cf6);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 20px;
      font-weight: 800;
      box-shadow: 0 4px 14px var(--primary-glow);
    }
    .brand-title {
      font-size: 22px;
      font-weight: 700;
      letter-spacing: -0.5px;
      background: linear-gradient(to right, #fff, #cbd5e1);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
    }
    .brand-sub {
      font-size: 12px;
      color: var(--text-muted);
    }
    .status-badge {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 6px 14px;
      border-radius: 20px;
      font-size: 13px;
      font-weight: 600;
      background: rgba(255, 255, 255, 0.04);
      border: 1px solid var(--card-border);
      transition: all 0.3s ease;
    }
    .status-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--warning);
      box-shadow: 0 0 8px var(--warning);
      animation: pulse 2s infinite;
    }
    .status-badge.connected .status-dot {
      background: var(--success);
      box-shadow: 0 0 10px var(--success-glow);
    }
    .status-badge.connected {
      border-color: rgba(16, 185, 129, 0.3);
      color: #34d399;
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 16px;
      padding: 24px;
      backdrop-filter: blur(16px);
      box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
    }
    .tabs {
      display: flex;
      background: rgba(0, 0, 0, 0.3);
      padding: 4px;
      border-radius: 12px;
      gap: 4px;
      margin-bottom: 20px;
      border: 1px solid rgba(255, 255, 255, 0.05);
    }
    .tab-btn {
      flex: 1;
      padding: 10px 14px;
      border: none;
      background: transparent;
      color: var(--text-muted);
      border-radius: 8px;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.2s ease;
      text-align: center;
    }
    .tab-btn.active {
      background: var(--primary);
      color: #ffffff;
      box-shadow: 0 2px 10px var(--primary-glow);
    }
    .tab-content {
      display: none;
    }
    .tab-content.active {
      display: block;
      animation: fadeIn 0.25s ease;
    }
    .form-group {
      display: flex;
      flex-direction: column;
      gap: 8px;
      margin-bottom: 18px;
    }
    label {
      font-size: 13px;
      font-weight: 600;
      color: var(--text-muted);
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
    .label-badge {
      font-size: 11px;
      padding: 2px 6px;
      background: rgba(255, 255, 255, 0.08);
      border-radius: 4px;
      color: #94a3b8;
    }
    input, textarea {
      width: 100%;
      background: var(--input-bg);
      border: 1px solid rgba(255, 255, 255, 0.12);
      border-radius: 10px;
      padding: 12px 14px;
      color: var(--text-main);
      font-size: 14px;
      outline: none;
      transition: border-color 0.2s ease, box-shadow 0.2s ease;
    }
    textarea {
      resize: vertical;
      min-height: 85px;
      font-family: monospace;
      font-size: 13px;
    }
    input:focus, textarea:focus {
      border-color: var(--primary);
      box-shadow: 0 0 0 3px var(--primary-glow);
    }
    .btn-row {
      display: flex;
      gap: 10px;
      margin-top: 8px;
    }
    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      padding: 12px 20px;
      border-radius: 10px;
      font-size: 14px;
      font-weight: 600;
      border: none;
      cursor: pointer;
      transition: all 0.2s ease;
    }
    .btn-primary {
      background: linear-gradient(135deg, #6366f1, #4f46e5);
      color: white;
      flex: 1;
      box-shadow: 0 4px 14px var(--primary-glow);
    }
    .btn-primary:hover:not(:disabled) {
      background: linear-gradient(135deg, #4f46e5, #4338ca);
      transform: translateY(-1px);
    }
    .btn-secondary {
      background: rgba(255, 255, 255, 0.08);
      color: var(--text-main);
      border: 1px solid var(--card-border);
    }
    .btn-secondary:hover:not(:disabled) {
      background: rgba(255, 255, 255, 0.12);
    }
    .btn:disabled {
      opacity: 0.5;
      cursor: not-allowed;
      transform: none;
    }
    .info-box {
      background: rgba(99, 102, 241, 0.08);
      border: 1px solid rgba(99, 102, 241, 0.2);
      border-radius: 12px;
      padding: 14px;
      font-size: 13px;
      line-height: 1.5;
      color: #c7d2fe;
      margin-bottom: 18px;
    }
    .info-box strong {
      color: #ffffff;
    }
    .code-box {
      background: rgba(0, 0, 0, 0.5);
      border: 1px solid rgba(255, 255, 255, 0.08);
      border-radius: 8px;
      padding: 10px 12px;
      font-family: monospace;
      font-size: 12px;
      word-break: break-all;
      color: #a5b4fc;
      user-select: all;
      margin-top: 6px;
    }
    .step-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
      margin: 14px 0;
    }
    .step-item {
      display: flex;
      gap: 12px;
      font-size: 13px;
      line-height: 1.4;
      color: var(--text-muted);
    }
    .step-num {
      width: 24px;
      height: 24px;
      border-radius: 50%;
      background: rgba(99, 102, 241, 0.2);
      color: #a5b4fc;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: 700;
      font-size: 12px;
      flex-shrink: 0;
    }
    .meta-card {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 12px;
      margin-top: 8px;
    }
    .meta-item {
      background: rgba(0, 0, 0, 0.2);
      padding: 10px 14px;
      border-radius: 10px;
      border: 1px solid rgba(255, 255, 255, 0.05);
    }
    .meta-label {
      font-size: 11px;
      color: var(--text-muted);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 4px;
    }
    .meta-val {
      font-size: 13px;
      font-weight: 600;
      color: #e2e8f0;
      font-family: monospace;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .console {
      background: #05070c;
      border: 1px solid rgba(255, 255, 255, 0.08);
      border-radius: 12px;
      padding: 14px;
      font-family: monospace;
      font-size: 12px;
      min-height: 90px;
      max-height: 160px;
      overflow-y: auto;
      color: #94a3b8;
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .console-entry {
      line-height: 1.4;
    }
    .console-entry.success { color: #34d399; }
    .console-entry.error { color: #f87171; }
    .console-entry.info { color: #818cf8; }
    .spinner {
      width: 16px;
      height: 16px;
      border: 2px solid rgba(255, 255, 255, 0.3);
      border-top-color: #ffffff;
      border-radius: 50%;
      animation: spin 0.8s linear infinite;
      display: inline-block;
    }
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
    @keyframes pulse {
      0%, 100% { opacity: 1; transform: scale(1); }
      50% { opacity: 0.5; transform: scale(0.9); }
    }
    @keyframes fadeIn {
      from { opacity: 0; transform: translateY(4px); }
      to { opacity: 1; transform: translateY(0); }
    }
  </style>
</head>
<body>

  <div class="container">
    <!-- Header -->
    <header class="header">
      <div class="brand">
        <div class="logo-icon">&#x26A1;</div>
        <div>
          <div class="brand-title">Zensu Server Portal</div>
          <div class="brand-sub">Cloudflare Clearance & Remote Session Sync</div>
        </div>
      </div>
      <div id="statusBadge" class="status-badge">
        <div class="status-dot"></div>
        <span id="statusText">Checking...</span>
      </div>
    </header>

    <!-- Main Tabs Card -->
    <main class="card">
      <div class="tabs">
        <button class="tab-btn active" onclick="switchTab('browserTab')">&#x1F310; Launch Browser</button>
        <button class="tab-btn" onclick="switchTab('linkTab')">&#x1F517; Paste Link</button>
        <button class="tab-btn" onclick="switchTab('cookieTab')">&#x1F36A; Direct Cookie</button>
        <button class="tab-btn" onclick="switchTab('mobileTab')">&#x1F4D6; Guide</button>
      </div>

      <!-- Tab 0: Launch Browser (Primary) -->
      <div id="browserTab" class="tab-content active">
        <div class="info-box">
          <strong>&#x1F680; Automatic Browser Verification:</strong>
          Click below to trigger the browser on the server. If Cloudflare displays a <em>"Verify you are human"</em> checkbox, click it. Zensu will automatically capture the clearance credentials, update the server, and activate streaming!
          <div style="margin-top: 10px; padding-top: 8px; border-top: 1px solid rgba(255,255,255,0.08); font-size: 12px; color: #94a3b8;">
            &#x1F4BB; <strong>Running on VPS / Docker?</strong> You can view and click the browser screen via <a href="/vnc.html" target="_blank" style="color: #818cf8; font-weight: 600; text-decoration: underline;">noVNC Virtual Desktop (:6080)</a>.
          </div>
        </div>

        <div class="form-group" style="margin-top: 14px;">
          <label for="browserSelect">
            Browser Engine to Launch
            <span class="label-badge">Select Engine</span>
          </label>
          <select id="browserSelect" style="width: 100%; background: var(--input-bg); border: 1px solid rgba(255,255,255,0.12); border-radius: 10px; padding: 12px 14px; color: var(--text-main); font-size: 14px; outline: none;">
            <option value="brave" selected>🦁 Brave Browser (Recommended: Privacy & Stealth)</option>
            <option value="edge">🌊 Microsoft Edge (High Compatibility)</option>
            <option value="chrome">🌐 Google Chrome Stable</option>
            <option value="auto">⚡ Auto-Detect Available Browser</option>
          </select>
        </div>

        <div class="btn-row" style="margin-top: 14px;">
          <button id="triggerBrowserBtn" class="btn btn-primary" onclick="triggerBrowser()">
            <span id="triggerBrowserBtnText">&#x1F310; Launch Browser to Solve Cloudflare</span>
          </button>
        </div>
      </div>

      <!-- Tab 1: Paste Link -->
      <div id="linkTab" class="tab-content">
        <div class="info-box">
          <strong>&#x1F4A1; Paste Tokenized Link:</strong> Open AnimePahe on your phone or laptop. Once the Cloudflare verification completes, copy the full URL from your browser address bar (it will contain <code>__cf_chl_tk=...</code>) and paste it below.
        </div>
        <div class="form-group">
          <label for="linkInput">
            AnimePahe Link with Token
            <span class="label-badge">URL with __cf_chl_tk</span>
          </label>
          <input type="url" id="linkInput" placeholder="https://animepahe.pw/?__cf_chl_tk=uXn5IrwvjJ5pn_2sd9..." autocomplete="off" spellcheck="false" />
        </div>
        <div class="btn-row">
          <button id="pasteBtn" class="btn btn-secondary" onclick="pasteFromClipboard('linkInput')">&#x1F4CB; Paste</button>
          <button id="submitLinkBtn" class="btn btn-primary" onclick="submitLink()">
            <span id="submitLinkBtnText">&#x26A1; Extract & Authenticate</span>
          </button>
        </div>
      </div>

      <!-- Tab 2: Direct Cookie / UA -->
      <div id="cookieTab" class="tab-content">
        <div class="info-box">
          <strong>Direct Cookie Upload:</strong> If you extracted cookies using DevTools (F12) or another tool, paste your <code>cf_clearance</code> cookie below. Note: Cloudflare clearance cookies are bound to the IP address where they were solved!
        </div>
        <div class="form-group">
          <label for="cfInput">
            cf_clearance Token or Cookie Header
            <span class="label-badge">Required</span>
          </label>
          <textarea id="cfInput" placeholder="Paste cf_clearance value (e.g. uXn5Irwvj...) or full Cookie header" spellcheck="false"></textarea>
        </div>
        <div class="form-group">
          <label for="uaInput">
            Browser User-Agent
            <span class="label-badge">Auto-detected</span>
          </label>
          <input type="text" id="uaInput" placeholder="User-Agent string" spellcheck="false" />
        </div>
        <div class="btn-row">
          <button id="fillUABtn" class="btn btn-secondary" onclick="fillLocalUA()">&#x1F504; My User-Agent</button>
          <button id="submitCookieBtn" class="btn btn-primary" onclick="submitCookies()">
            <span id="submitCookieBtnText">&#x1F4BE; Save & Authenticate</span>
          </button>
        </div>
      </div>

      <!-- Tab 3: Guide -->
      <div id="mobileTab" class="tab-content">
        <div class="info-box">
          <strong>How Cloudflare Clearance Works:</strong>
          Cloudflare's <code>cf_clearance</code> cookie is marked <code>HttpOnly</code> and cryptographically locked to the client's IP address.
        </div>
        <div class="step-list">
          <div class="step-item">
            <div class="step-num">1</div>
            <div><strong>Best Method:</strong> Use the <strong>Launch Browser</strong> tab above. The browser runs directly on the server's IP, so the clearance matches 100%.</div>
          </div>
          <div class="step-item">
            <div class="step-num">2</div>
            <div><strong>Mobile History Trick:</strong> On your phone, open <a href="https://animepahe.pw" target="_blank" style="color: #818cf8; font-weight: 600;">https://animepahe.pw</a>. Solve the challenge. Check your browser history for the URL with <code>?__cf_chl_tk=...</code>, copy it, and paste it in the <strong>Paste Link</strong> tab!</div>
          </div>
          <div class="step-item">
            <div class="step-num">3</div>
            <div><strong>PC DevTools:</strong> On laptop/desktop Chrome, press F12 &rarr; Application &rarr; Cookies &rarr; copy <code>cf_clearance</code>.</div>
          </div>
        </div>
      </div>
    </main>

    <!-- Server Meta Info -->
    <div class="card" style="padding: 16px 20px;">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
        <span style="font-size: 13px; font-weight: 700; color: #cbd5e1;">Active Credentials Status</span>
        <button class="btn btn-secondary" style="padding: 6px 12px; font-size: 12px;" onclick="testConnection()">&#x21BB; Test Connection</button>
      </div>
      <div class="meta-card">
        <div class="meta-item">
          <div class="meta-label">Domain</div>
          <div id="metaDomain" class="meta-val">https://animepahe.pw</div>
        </div>
        <div class="meta-item">
          <div class="meta-label">cf_clearance</div>
          <div id="metaCF" class="meta-val">Not configured</div>
        </div>
        <div class="meta-item">
          <div class="meta-label">User-Agent</div>
          <div id="metaUA" class="meta-val">Not configured</div>
        </div>
      </div>
    </div>

    <!-- Live Console -->
    <div class="card" style="padding: 16px 20px;">
      <div style="font-size: 12px; font-weight: 700; color: #94a3b8; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.5px;">Activity Log</div>
      <div id="console" class="console">
        <div class="console-entry info">[Init] Zensu portal ready. Connecting to server...</div>
      </div>
    </div>
  </div>

  <script>
    function log(msg, type = '') {
      const con = document.getElementById('console');
      const entry = document.createElement('div');
      entry.className = 'console-entry ' + type;
      const time = new Date().toLocaleTimeString();
      entry.textContent = '[' + time + '] ' + msg;
      con.appendChild(entry);
      con.scrollTop = con.scrollHeight;
    }

    function switchTab(tabId) {
      document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
      document.querySelectorAll('.tab-btn').forEach(el => el.classList.remove('active'));
      const target = document.getElementById(tabId);
      if (target) target.classList.add('active');
      const tabs = ['browserTab', 'linkTab', 'cookieTab', 'mobileTab'];
      const idx = tabs.indexOf(tabId);
      if (idx !== -1) {
        const btns = document.querySelectorAll('.tab-btn');
        if (btns[idx]) btns[idx].classList.add('active');
      }
    }

    async function triggerBrowser() {
      const browserSelect = document.getElementById('browserSelect');
      const browserChoice = browserSelect ? browserSelect.value : 'brave';
      const btn = document.getElementById('triggerBrowserBtn');
      const btnText = document.getElementById('triggerBrowserBtnText');
      btn.disabled = true;
      btnText.innerHTML = '<span class="spinner"></span> ' + browserChoice.toUpperCase() + ' Active & Waiting...';
      log('Launching ' + browserChoice + ' browser on server to solve Cloudflare challenge...', 'info');

      try {
        const res = await fetch('/api/portal/trigger-browser', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ browser: browserChoice })
        });
        const data = await res.json();
        if (data.success) {
          log(data.message, 'success');
          alert('Success! ' + data.message);
          updateStatus();
        } else {
          log('Browser verification failed: ' + (data.error || 'Unknown error'), 'error');
          alert('Browser verification failed: ' + data.error);
        }
      } catch (err) {
        log('Request failed: ' + err.message, 'error');
        alert('Server request failed: ' + err.message);
      } finally {
        btn.disabled = false;
        btnText.innerHTML = '&#x1F310; Launch Browser to Solve Cloudflare';
      }
    }

    function fillLocalUA() {
      const ua = navigator.userAgent;
      document.getElementById('uaInput').value = ua;
      log('Filled User-Agent from this device (' + ua.substring(0, 40) + '...)');
    }

    async function pasteFromClipboard(targetId) {
      try {
        if (navigator.clipboard && navigator.clipboard.readText) {
          const text = await navigator.clipboard.readText();
          document.getElementById(targetId).value = text;
          log('Pasted from clipboard!');
        } else {
          document.getElementById(targetId).focus();
          log('Clipboard API unavailable in this browser; please paste manually (Ctrl+V / Long-press).', 'info');
        }
      } catch (err) {
        document.getElementById(targetId).focus();
        log('Clipboard access denied; please paste manually into the input box.', 'info');
      }
    }

    function copyBookmarklet() {
      const code = document.getElementById('bookmarkletCode').innerText;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(code).then(() => log('Copied mobile script to clipboard!', 'success'));
      } else {
        log('Select the code box above to copy.', 'info');
      }
    }

    async function updateStatus() {
      try {
        const res = await fetch('/api/portal/status');
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        
        const badge = document.getElementById('statusBadge');
        const text = document.getElementById('statusText');
        const metaDomain = document.getElementById('metaDomain');
        const metaCF = document.getElementById('metaCF');
        const metaUA = document.getElementById('metaUA');

        metaDomain.textContent = data.domain || 'https://animepahe.pw';
        metaCF.textContent = data.cf_preview || 'Not configured';
        metaUA.textContent = data.ua_preview || 'Not configured';

        if (data.authenticated) {
          badge.className = 'status-badge connected';
          text.textContent = 'Active & Connected';
        } else {
          badge.className = 'status-badge';
          text.textContent = 'Action Required';
        }
      } catch (e) {
        document.getElementById('statusText').textContent = 'Server Unreachable';
      }
    }

    async function submitLink() {
      const linkInput = document.getElementById('linkInput');
      const url = linkInput.value.trim();
      if (!url) {
        alert('Please paste an AnimePahe link first!');
        return;
      }

      const btn = document.getElementById('submitLinkBtn');
      const btnText = document.getElementById('submitLinkBtnText');
      btn.disabled = true;
      btnText.innerHTML = '<span class="spinner"></span> Resolving Cloudflare...';
      log('Sending link to server. Server browser is navigating and extracting clearance...', 'info');

      try {
        const res = await fetch('/api/portal/submit-link', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url })
        });
        const data = await res.json();
        if (data.success) {
          log(data.message, 'success');
          alert('Success! Server authenticated and streaming is ready.');
          linkInput.value = '';
          updateStatus();
        } else {
          log('Extraction error: ' + (data.error || 'Unknown error'), 'error');
          alert('Error: ' + data.error);
        }
      } catch (err) {
        log('Request failed: ' + err.message, 'error');
        alert('Server request failed: ' + err.message);
      } finally {
        btn.disabled = false;
        btnText.innerHTML = '&#x26A1; Extract & Authenticate';
      }
    }

    async function submitCookies() {
      const cf = document.getElementById('cfInput').value.trim();
      let ua = document.getElementById('uaInput').value.trim();
      if (!cf) {
        alert('Please paste your cf_clearance value or Cookie header!');
        return;
      }
      if (!ua) {
        ua = navigator.userAgent;
      }

      const btn = document.getElementById('submitCookieBtn');
      const btnText = document.getElementById('submitCookieBtnText');
      btn.disabled = true;
      btnText.innerHTML = '<span class="spinner"></span> Verifying...';
      log('Submitting credentials and testing connection with AnimePahe...', 'info');

      try {
        const res = await fetch('/api/portal/submit-cookies', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ cf, ua, cookies: cf })
        });
        const data = await res.json();
        if (data.success) {
          log(data.message, 'success');
          alert('Success! ' + data.message);
          document.getElementById('cfInput').value = '';
          updateStatus();
        } else {
          log('Verification error: ' + (data.error || 'Unknown error'), 'error');
          alert('Error: ' + data.error);
        }
      } catch (err) {
        log('Request failed: ' + err.message, 'error');
        alert('Server request failed: ' + err.message);
      } finally {
        btn.disabled = false;
        btnText.innerHTML = '&#x1F4BE; Save & Authenticate';
      }
    }

    async function testConnection() {
      log('Testing connection to AnimePahe...', 'info');
      try {
        const res = await fetch('/api/portal/test', { method: 'POST' });
        const data = await res.json();
        if (data.success) {
          log(data.message, 'success');
        } else {
          log(data.error || 'Connection test failed', 'error');
        }
        updateStatus();
      } catch (e) {
        log('Connection test failed: ' + e.message, 'error');
      }
    }

    // Auto-fill UA and check status on page load
    fillLocalUA();
    updateStatus();
    setInterval(updateStatus, 10000);
  </script>
</body>
</html>`
