package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionCookieName = "negotiable_session"

type Config struct {
	WebOrigin   string
	RequestLimit int
	AuthLimit    int
	Window       time.Duration
	MaxKeys      int
}

type bucket struct {
	count   int
	resetAt time.Time
}

type Middleware struct {
	next          http.Handler
	webOrigin     string
	requestLimit  int
	authLimit     int
	window        time.Duration
	maxKeys       int
	now           func() time.Time
	mu            sync.Mutex
	buckets       map[string]bucket
}

func New(next http.Handler, config Config) http.Handler {
	if config.RequestLimit <= 0 {
		config.RequestLimit = 12
	}
	if config.AuthLimit <= 0 {
		config.AuthLimit = 30
	}
	if config.Window <= 0 {
		config.Window = time.Minute
	}
	if config.MaxKeys <= 0 {
		config.MaxKeys = 4096
	}
	return &Middleware{
		next: next, webOrigin: normalizeOrigin(config.WebOrigin),
		requestLimit: config.RequestLimit, authLimit: config.AuthLimit,
		window: config.Window, maxKeys: config.MaxKeys,
		now: time.Now, buckets: make(map[string]bucket),
	}
}

func (middleware *Middleware) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(response.Header())

	origin := normalizeOrigin(request.Header.Get("Origin"))
	if request.Method == http.MethodOptions && middleware.webOrigin != "" && origin != middleware.webOrigin {
		writeError(response, http.StatusForbidden, "cross-origin request denied")
		return
	}
	if isUnsafe(request.Method) && hasSessionCookie(request) {
		if middleware.webOrigin == "" || origin != middleware.webOrigin {
			writeError(response, http.StatusForbidden, "cross-origin request denied")
			return
		}
	}

	key, limit := middleware.rateLimitKey(request)
	if key != "" {
		allowed, retryAfter := middleware.allow(key, limit)
		if !allowed {
			response.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
			writeError(response, http.StatusTooManyRequests, "too many requests")
			return
		}
	}

	middleware.next.ServeHTTP(response, request)
}

func (middleware *Middleware) rateLimitKey(request *http.Request) (string, int) {
	if request.Method == http.MethodPost && request.URL.Path == "/api/v1/requests" {
		if cookie, err := request.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			sum := sha256.Sum256([]byte(cookie.Value))
			return "request:session:" + hex.EncodeToString(sum[:]), middleware.requestLimit
		}
		return "request:client:" + clientAddress(request.RemoteAddr), middleware.requestLimit
	}
	if isOAuthEndpoint(request.Method, request.URL.Path) {
		return "oauth:client:" + clientAddress(request.RemoteAddr), middleware.authLimit
	}
	return "", 0
}

func (middleware *Middleware) allow(key string, limit int) (bool, time.Duration) {
	now := middleware.now().UTC()
	middleware.mu.Lock()
	defer middleware.mu.Unlock()

	current, exists := middleware.buckets[key]
	if !exists || !now.Before(current.resetAt) {
		if !exists && len(middleware.buckets) >= middleware.maxKeys {
			for candidate, value := range middleware.buckets {
				if !now.Before(value.resetAt) {
					delete(middleware.buckets, candidate)
				}
			}
			if len(middleware.buckets) >= middleware.maxKeys {
				return false, middleware.window
			}
		}
		middleware.buckets[key] = bucket{count: 1, resetAt: now.Add(middleware.window)}
		return true, 0
	}
	if current.count >= limit {
		return false, current.resetAt.Sub(now)
	}
	current.count++
	middleware.buckets[key] = current
	return true, 0
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func clientAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if parsed := net.ParseIP(remoteAddr); parsed != nil {
		return parsed.String()
	}
	return "unknown"
}

func hasSessionCookie(request *http.Request) bool {
	cookie, err := request.Cookie(sessionCookieName)
	return err == nil && cookie.Value != ""
}

func isUnsafe(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isOAuthEndpoint(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	switch path {
	case "/api/v1/auth/google/login", "/api/v1/auth/google/callback",
		"/api/v1/calendar/google/connect", "/api/v1/calendar/google/callback":
		return true
	default:
		return false
	}
}

func setSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	header.Set("Cache-Control", "no-store")
}

func writeError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}
