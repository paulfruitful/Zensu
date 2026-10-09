package server

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sync"
	"time"

	"zensu/internal/api"
	"zensu/internal/config"
	"zensu/internal/kwik"
	"zensu/internal/logger"
)

var nonAlphanumRe = regexp.MustCompile(`[^\w ,+\-()\s]`)

func sanitizeName(name string) string {
	name = nonAlphanumRe.ReplaceAllString(name, " ")
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
	return strings.TrimSpace(name)
}

// IsExpiredError returns true if the error indicates that Cloudflare clearance or cookies have expired or been blocked.
func IsExpiredError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "403") ||
		strings.Contains(msg, "cf blocked") ||
		strings.Contains(msg, "refresh cookies") ||
		strings.Contains(msg, "clearance") ||
		strings.Contains(msg, "forbidden")
}

type CredentialsRefresher func() (*api.Client, *kwik.Extractor, error)

type Server struct {
	mu          sync.RWMutex
	client      *api.Client
	extractor   *kwik.Extractor
	cfg         *config.Config
	refresher   CredentialsRefresher
	refreshMu   sync.Mutex
	lastRefresh time.Time
}

func NewServer(client *api.Client, extractor *kwik.Extractor, cfg *config.Config, refresher CredentialsRefresher) *Server {
	return &Server{
		client:    client,
		extractor: extractor,
		cfg:       cfg,
		refresher: refresher,
	}
}

func (s *Server) Client() *api.Client {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *Server) Extractor() *kwik.Extractor {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.extractor
}

func (s *Server) Config() *config.Config {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) SetCredentials(client *api.Client, extractor *kwik.Extractor) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
	s.extractor = extractor
}

func (s *Server) hasRefresher() bool {
	return s != nil && s.refresher != nil
}

func (s *Server) RefreshCredentials() error {
	if !s.hasRefresher() {
		return fmt.Errorf("no refresher configured")
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	// Double check: if refreshed within last 5 seconds and connection is valid, skip re-launching browser
	if !s.lastRefresh.IsZero() && time.Since(s.lastRefresh) < 5*time.Second {
		currentClient := s.Client()
		if currentClient != nil && currentClient.TestConnection() == nil {
			logger.Infof("SERVER_REFRESH_SKIP", "Credentials refreshed recently and connection valid; skipping redundant browser launch")
			return nil
		}
	}

	logger.Infof("SERVER_REFRESH_START", "Launching browser to refresh Cloudflare credentials...")
	newClient, newExtractor, err := s.refresher()
	if err != nil {
		logger.Errorf("SERVER_REFRESH_FAIL", "Failed to refresh credentials: %v", err)
		return err
	}

	s.lastRefresh = time.Now()
	s.SetCredentials(newClient, newExtractor)
	logger.Infof("SERVER_REFRESH_SUCCESS", "Successfully refreshed credentials and reinitialized clients")
	return nil
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", handleSearch(s))
	mux.HandleFunc("/api/episodes", handleEpisodes(s))
	mux.HandleFunc("/api/stream", handleStream(s))

	// Cloudflare Clearance Web Portal endpoints
	mux.HandleFunc("/portal", handlePortal(s))
	mux.HandleFunc("/api/portal/status", handlePortalStatus(s))
	mux.HandleFunc("/api/portal/submit-link", handlePortalSubmitLink(s))
	mux.HandleFunc("/api/portal/submit-cookies", handlePortalSubmitCookies(s))
	mux.HandleFunc("/api/portal/test", handlePortalTest(s))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			handlePortal(s)(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return corsMiddleware(mux)
}

func NewRouter(client *api.Client, extractor *kwik.Extractor, cfg *config.Config, refresher ...CredentialsRefresher) http.Handler {
	var ref CredentialsRefresher
	if len(refresher) > 0 {
		ref = refresher[0]
	}
	s := NewServer(client, extractor, cfg, ref)
	return s.Router()
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleSearch(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Error(w, "missing parameter 'q'", http.StatusBadRequest)
			return
		}

		logger.Infof("SERVER_SEARCH", "Searching for anime matching %q", q)
		client := s.Client()
		if client == nil {
			http.Error(w, "Cloudflare clearance not configured. Visit /portal to authenticate.", http.StatusServiceUnavailable)
			return
		}

		results, err := client.Search(q)
		if err != nil && IsExpiredError(err) && s.hasRefresher() {
			logger.Warnf("SERVER_SEARCH_CF_EXPIRED", "Search blocked by Cloudflare (expired cookie): %v. Launching browser to refresh...", err)
			if refErr := s.RefreshCredentials(); refErr == nil {
				client = s.Client()
				if client != nil {
					results, err = client.Search(q)
				}
			}
		}

		if err != nil {
			logger.Errorf("SERVER_SEARCH_ERR", "Search failed: %v", err)
			http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func handleEpisodes(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("slug")
		if slug == "" {
			http.Error(w, "missing parameter 'slug'", http.StatusBadRequest)
			return
		}

		logger.Infof("SERVER_EPISODES", "Fetching episodes for slug %s", slug)
		client := s.Client()
		if client == nil {
			http.Error(w, "Cloudflare clearance not configured. Visit /portal to authenticate.", http.StatusServiceUnavailable)
			return
		}

		episodes, err := client.GetEpisodes(slug)
		if err != nil && IsExpiredError(err) && s.hasRefresher() {
			logger.Warnf("SERVER_EPISODES_CF_EXPIRED", "Episodes request blocked by Cloudflare: %v. Launching browser to refresh...", err)
			if refErr := s.RefreshCredentials(); refErr == nil {
				client = s.Client()
				if client != nil {
					episodes, err = client.GetEpisodes(slug)
				}
			}
		}

		if err != nil {
			logger.Errorf("SERVER_EPISODES_ERR", "Failed to fetch episodes: %v", err)
			http.Error(w, fmt.Sprintf("failed to fetch episodes: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(episodes)
	}
}

func handleStream(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxyURL := r.URL.Query().Get("proxy_url")
		cfg := s.Config()
		ua := ""
		cookies := ""
		if cfg != nil {
			ua = cfg.UA
			cookies = cfg.Cookies
		}

		if proxyURL != "" {
			logger.Infof("SERVER_STREAM_PROXY_RAW", "Proxying raw chunk/stream: %s", proxyURL)
			proxyStream(w, r, proxyURL, ua)
			return
		}

		slug := r.URL.Query().Get("slug")
		epSession := r.URL.Query().Get("session")
		title := r.URL.Query().Get("title")

		if slug == "" || epSession == "" {
			http.Error(w, "missing required parameters 'slug' and 'session'", http.StatusBadRequest)
			return
		}

		quality := r.URL.Query().Get("quality")
		if quality == "" && cfg != nil {
			quality = cfg.Quality
		}
		audio := r.URL.Query().Get("audio")
		if audio == "" && cfg != nil {
			audio = cfg.Audio
		}

		logger.Infof("SERVER_STREAM_REQ", "Stream requested for slug: %s, session: %s, title: %q", slug, epSession, title)

		client := s.Client()
		if client == nil {
			http.Error(w, "client not initialized", http.StatusInternalServerError)
			return
		}

		if title != "" {
			sanitizedTitle := sanitizeName(title)
			episodes, err := client.GetEpisodes(slug)
			if err != nil && IsExpiredError(err) && s.hasRefresher() {
				logger.Warnf("SERVER_STREAM_CF_EXPIRED", "GetEpisodes blocked by Cloudflare: %v. Launching browser to refresh...", err)
				if refErr := s.RefreshCredentials(); refErr == nil {
					client = s.Client()
					if client != nil {
						episodes, err = client.GetEpisodes(slug)
					}
				}
			}

			if err == nil {
				var epNum float64
				found := false
				for _, ep := range episodes {
					if ep.Session == epSession {
						epNum = ep.Episode
						found = true
						break
					}
				}

				if found {
					epStr := fmt.Sprintf("E%02.0f", epNum)
					if math.Mod(epNum, 1) != 0 {
						epStr = fmt.Sprintf("E%.1f", epNum)
					}

					downloadDir := ""
					if cfg != nil {
						downloadDir = cfg.DownloadDir
					}
					if strings.HasPrefix(downloadDir, "~/") {
						home, _ := os.UserHomeDir()
						downloadDir = home + downloadDir[1:]
					}

					localPath := filepath.Join(downloadDir, sanitizedTitle, sanitizedTitle+" "+epStr+".mp4")
					if _, err := os.Stat(localPath); err == nil {
						logger.Infof("SERVER_STREAM_STATIC", "Serving local static file: %s", localPath)
						http.ServeFile(w, r, localPath)
						return
					}
				}
			}
		}

		candidates, err := client.GetKwikLinks(slug, epSession)
		if err != nil && IsExpiredError(err) && s.hasRefresher() {
			logger.Warnf("SERVER_STREAM_CF_EXPIRED", "GetKwikLinks blocked by Cloudflare: %v. Launching browser to refresh...", err)
			if refErr := s.RefreshCredentials(); refErr == nil {
				client = s.Client()
				if client != nil {
					candidates, err = client.GetKwikLinks(slug, epSession)
				}
			}
		}

		if err != nil {
			logger.Errorf("SERVER_STREAM_KWIK_ERR", "Failed to resolve kwik links: %v", err)
			http.Error(w, fmt.Sprintf("failed to resolve links: %v", err), http.StatusInternalServerError)
			return
		}
		if len(candidates) == 0 {
			http.Error(w, "no links found for requested episode", http.StatusNotFound)
			return
		}

		kwikURL := api.SelectBestKwik(candidates, quality, audio)
		if kwikURL == "" {
			http.Error(w, "no candidate matching requested quality/audio", http.StatusNotFound)
			return
		}

		extractor := s.Extractor()
		if extractor == nil {
			http.Error(w, "extractor not initialized", http.StatusInternalServerError)
			return
		}

		dlURL, isHLS, err := extractor.GetDownloadURL(kwikURL)
		if err != nil && IsExpiredError(err) && s.hasRefresher() {
			logger.Warnf("SERVER_STREAM_CF_EXPIRED", "Extractor blocked by Cloudflare: %v. Launching browser to refresh...", err)
			if refErr := s.RefreshCredentials(); refErr == nil {
				extractor = s.Extractor()
				if extractor != nil {
					dlURL, isHLS, err = extractor.GetDownloadURL(kwikURL)
				}
			}
		}

		if err != nil {
			logger.Errorf("SERVER_STREAM_EXTRACT_ERR", "Failed to extract direct download URL: %v", err)
			http.Error(w, fmt.Sprintf("extraction failed: %v", err), http.StatusInternalServerError)
			return
		}

		// Re-fetch current config for UA and cookies
		cfg = s.Config()
		if cfg != nil {
			ua = cfg.UA
			cookies = cfg.Cookies
		}

		if isHLS {
			logger.Infof("SERVER_STREAM_HLS", "Proxying and rewriting HLS playlist: %s", dlURL)
			parsedBaseURL, err := url.Parse(dlURL)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid remote playlist URL: %v", err), http.StatusInternalServerError)
				return
			}

			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, dlURL, nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Referer", "https://kwik.cx/")
			if cookies != "" {
				req.Header.Set("Cookie", cookies)
			}

			httpClient := &http.Client{}
			resp, err := httpClient.Do(req)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to fetch remote playlist: %v", err), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()

			playlistBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to read remote playlist: %v", err), http.StatusInternalServerError)
				return
			}

			rewritten := rewriteM3U8(string(playlistBytes), parsedBaseURL, r.Host)

			w.Header().Set("Content-Type", "application/x-mpegURL")
			w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(rewritten))
			return
		}

		logger.Infof("SERVER_STREAM_PROXY", "Proxying remote stream: %s", dlURL)
		proxyStream(w, r, dlURL, ua)
	}
}

func proxyStream(w http.ResponseWriter, r *http.Request, rawURL string, ua string) {
	ctx := r.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", "https://kwik.cx/")
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorf("SERVER_PROXY_ERR", "Proxy request failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if rangeHeader := resp.Header.Get("Content-Range"); rangeHeader != "" {
		w.Header().Set("Content-Range", rangeHeader)
	}
	if lengthHeader := resp.Header.Get("Content-Length"); lengthHeader != "" {
		w.Header().Set("Content-Length", lengthHeader)
	}
	if acceptRanges := resp.Header.Get("Accept-Ranges"); acceptRanges != "" {
		w.Header().Set("Accept-Ranges", acceptRanges)
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func rewriteM3U8(content string, baseURL *url.URL, requestHost string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			// Check if it's a tag that has a URI parameter, e.g. URI="something"
			if strings.Contains(trimmed, "URI=") {
				re := regexp.MustCompile(`URI="([^"]+)"`)
				lines[i] = re.ReplaceAllStringFunc(trimmed, func(match string) string {
					sub := re.FindStringSubmatch(match)
					if len(sub) < 2 {
						return match
					}
					uVal := sub[1]
					resolved := resolveURL(uVal, baseURL)
					proxyURL := fmt.Sprintf("http://%s/api/stream?proxy_url=%s", requestHost, url.QueryEscape(resolved))
					return fmt.Sprintf(`URI="%s"`, proxyURL)
				})
			}
			continue
		}

		// Otherwise, it's a segment/playlist URL
		resolved := resolveURL(trimmed, baseURL)
		lines[i] = fmt.Sprintf("http://%s/api/stream?proxy_url=%s", requestHost, url.QueryEscape(resolved))
	}
	return strings.Join(lines, "\n")
}

func resolveURL(ref string, base *url.URL) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}
