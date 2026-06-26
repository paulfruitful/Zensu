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

func NewRouter(client *api.Client, extractor *kwik.Extractor, cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", handleSearch(client))
	mux.HandleFunc("/api/episodes", handleEpisodes(client))
	mux.HandleFunc("/api/stream", handleStream(client, extractor, cfg))
	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleSearch(client *api.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Error(w, "missing parameter 'q'", http.StatusBadRequest)
			return
		}

		logger.Infof("SERVER_SEARCH", "Searching for anime matching %q", q)
		results, err := client.Search(q)
		if err != nil {
			logger.Errorf("SERVER_SEARCH_ERR", "Search failed: %v", err)
			http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func handleEpisodes(client *api.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("slug")
		if slug == "" {
			http.Error(w, "missing parameter 'slug'", http.StatusBadRequest)
			return
		}

		logger.Infof("SERVER_EPISODES", "Fetching episodes for slug %s", slug)
		episodes, err := client.GetEpisodes(slug)
		if err != nil {
			logger.Errorf("SERVER_EPISODES_ERR", "Failed to fetch episodes: %v", err)
			http.Error(w, fmt.Sprintf("failed to fetch episodes: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(episodes)
	}
}

func handleStream(client *api.Client, extractor *kwik.Extractor, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxyURL := r.URL.Query().Get("proxy_url")
		if proxyURL != "" {
			logger.Infof("SERVER_STREAM_PROXY_RAW", "Proxying raw chunk/stream: %s", proxyURL)
			proxyStream(w, r, proxyURL, cfg.UA)
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
		if quality == "" {
			quality = cfg.Quality
		}
		audio := r.URL.Query().Get("audio")
		if audio == "" {
			audio = cfg.Audio
		}

		logger.Infof("SERVER_STREAM_REQ", "Stream requested for slug: %s, session: %s, title: %q", slug, epSession, title)

		if title != "" {
			sanitizedTitle := sanitizeName(title)
			episodes, err := client.GetEpisodes(slug)
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

					downloadDir := cfg.DownloadDir
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

		dlURL, isHLS, err := extractor.GetDownloadURL(kwikURL)
		if err != nil {
			logger.Errorf("SERVER_STREAM_EXTRACT_ERR", "Failed to extract direct download URL: %v", err)
			http.Error(w, fmt.Sprintf("extraction failed: %v", err), http.StatusInternalServerError)
			return
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
			req.Header.Set("User-Agent", cfg.UA)
			req.Header.Set("Referer", "https://kwik.cx/")
			if cfg.Cookies != "" {
				req.Header.Set("Cookie", cfg.Cookies)
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
		proxyStream(w, r, dlURL, cfg.UA)
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
