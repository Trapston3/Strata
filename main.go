package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxLogEntries     = 50
	listenAddr        = ":3000"
	maxRequestBody    = 1 << 20
	maxHeaderBytes    = 1 << 20
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

var (
	startedAt = time.Now()
	logger    = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	}))
)

type LogEntry struct {
	ID           string  `json:"id"`
	Timestamp    string  `json:"timestamp"`
	Method       string  `json:"method"`
	URL          string  `json:"url"`
	Status       int     `json:"status"`
	ResponseTime float64 `json:"responseTime"`
	Service      string  `json:"service"`
	Level        string  `json:"level"`
	Message      string  `json:"message"`
	ErrorCode    string  `json:"errorCode"`
	Path         string  `json:"path"`
	StatusCode   int     `json:"statusCode"`
	ResponseMS   float64 `json:"responseTimeMs"`
	LatencyMS    float64 `json:"latencyMs"`
	ClientIP     string  `json:"clientIp"`
}

type logBuffer struct {
	mu      sync.RWMutex
	entries [maxLogEntries]LogEntry
	index   int
	count   int
}

func (b *logBuffer) Append(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.index] = entry
	b.index = (b.index + 1) % maxLogEntries
	if b.count < maxLogEntries {
		b.count++
	}
}

func (b *logBuffer) List() []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return []LogEntry{}
	}

	result := make([]LogEntry, 0, b.count)
	pos := b.index - 1
	for i := 0; i < b.count; i++ {
		if pos < 0 {
			pos = maxLogEntries - 1
		}
		result = append(result, b.entries[pos])
		pos--
	}

	return result
}

func (b *logBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries = [maxLogEntries]LogEntry{}
	b.index = 0
	b.count = 0
}

type metricsStore struct {
	mu       sync.RWMutex
	counters map[string]uint64
	total    atomic.Uint64
}

func newMetricsStore() *metricsStore {
	return &metricsStore{
		counters: make(map[string]uint64),
	}
}

func (m *metricsStore) Observe(methodName, requestPath string, statusCode int) {
	m.total.Add(1)

	key := methodName + ":" + requestPath + ":" + strconv.Itoa(statusCode)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key]++
}

func (m *metricsStore) Render() string {
	var lines []string

	lines = append(lines,
		"# HELP strata_http_requests_total Total number of HTTP requests served by Strata.",
		"# TYPE strata_http_requests_total counter",
		fmt.Sprintf("strata_http_requests_total %d", m.total.Load()),
		"# HELP strata_goroutines_active Number of active goroutines.",
		"# TYPE strata_goroutines_active gauge",
		fmt.Sprintf("strata_goroutines_active %d", runtime.NumGoroutine()),
		"# HELP http_requests_total Total number of HTTP requests.",
		"# TYPE http_requests_total counter",
	)

	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.counters))
	for key := range m.counters {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"http_requests_total{method=%q,path=%q,status=%q} %d",
			parts[0], parts[1], parts[2], m.counters[key],
		))
	}

	return strings.Join(lines, "\n") + "\n"
}

type incident struct {
	ID              string `json:"id"`
	Service         string `json:"service"`
	ErrorCode       string `json:"errorCode"`
	EventCount      int    `json:"eventCount"`
	LatestTimestamp string `json:"latestTimestamp"`
	Score           int    `json:"score"`
	Severity        string `json:"severity"`
	Summary         string `json:"summary"`
}

type incidentResponse struct {
	Incidents []incident `json:"incidents"`
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

var (
	logsBuffer = &logBuffer{}
	metrics    = newMetricsStore()
	reqIDSeq   atomic.Uint64
)

func main() {
	mux := http.NewServeMux()
	registerRoutes(mux)
	server := newServer(mux)

	logger.Info("strata-api listening on :3000")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              listenAddr,
		Handler:           securityHeadersMiddleware(bodyLimitMiddleware(loggingMiddleware(handler))),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", methodHandler(http.MethodGet, healthHandler))
	mux.HandleFunc("/metrics", methodHandler(http.MethodGet, metricsHandler))
	mux.HandleFunc("/api/logs", logsRouteHandler)
	mux.HandleFunc("/api/incidents", methodHandler(http.MethodGet, incidentsHandler))
	mux.HandleFunc("/", spaHandler("public"))
}

func methodHandler(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func logsRouteHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getLogsHandler(w, r)
	case http.MethodDelete:
		deleteLogsHandler(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		reqID := requestID(r)

		next.ServeHTTP(recorder, r)

		statusCode := recorder.status
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		requestPath := sanitizePath(r.URL.Path)
		latencyMS := float64(time.Since(start).Microseconds()) / 1000
		clientIP := clientIP(r)
		entry := buildLogEntry(reqID, r.Method, requestPath, statusCode, latencyMS, clientIP)

		logsBuffer.Append(entry)
		metrics.Observe(r.Method, requestPath, statusCode)

		level := slog.LevelInfo
		if statusCode >= 500 {
			level = slog.LevelError
		} else if statusCode >= 400 {
			level = slog.LevelWarn
		}

		logger.LogAttrs(
			r.Context(),
			level,
			"request observed",
			slog.String("reqId", reqID),
			slog.String("method", r.Method),
			slog.String("path", requestPath),
			slog.Int("statusCode", statusCode),
			slog.Float64("latencyMs", latencyMS),
			slog.String("clientIp", clientIP),
		)
	})
}

func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	writeJSON(w, http.StatusOK, map[string]any{
		"uptime":    time.Since(startedAt).Seconds(),
		"processID": os.Getpid(),
		"memory": map[string]uint64{
			"heapUsed": mem.Alloc,
			"rss":      mem.Sys,
		},
	})
}

func metricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, metrics.Render())
}

func getLogsHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, logsBuffer.List())
}

func deleteLogsHandler(w http.ResponseWriter, _ *http.Request) {
	logsBuffer.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func incidentsHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, incidentResponse{Incidents: buildIncidents(logsBuffer.List())})
}

func spaHandler(publicDir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(publicDir))
	indexPath := path.Join(publicDir, "index.html")

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		target := path.Join(publicDir, cleanPath)
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	}
}

func buildLogEntry(reqID, methodName, requestPath string, statusCode int, responseTimeMS float64, clientIP string) LogEntry {
	service := inferService(requestPath)
	level := "info"
	if statusCode >= 500 {
		level = "error"
	} else if statusCode >= 400 {
		level = "warn"
	}

	latency := roundMillis(responseTimeMS)

	return LogEntry{
		ID:           fmt.Sprintf("%s-%d", reqID, time.Now().UnixMilli()),
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		Method:       methodName,
		URL:          requestPath,
		Status:       statusCode,
		ResponseTime: latency,
		Service:      service,
		Level:        level,
		Message:      fmt.Sprintf("%s %s -> %d in %.2fms", methodName, requestPath, statusCode, latency),
		ErrorCode:    inferErrorCode(statusCode, requestPath),
		Path:         requestPath,
		StatusCode:   statusCode,
		ResponseMS:   latency,
		LatencyMS:    latency,
		ClientIP:     clientIP,
	}
}

func sanitizePath(rawPath string) string {
	cleaned := strings.TrimSpace(rawPath)
	if cleaned == "" {
		return "/"
	}

	if i := strings.IndexAny(cleaned, "?#"); i >= 0 {
		cleaned = cleaned[:i]
	}

	cleaned = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, cleaned)
	if cleaned == "" {
		return "/"
	}

	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}

	normalized := path.Clean(cleaned)
	if normalized == "." || normalized == "" {
		return "/"
	}
	return normalized
}

func inferService(requestPath string) string {
	value := strings.ToLower(requestPath)
	if strings.Contains(value, "auth") {
		return "auth"
	}
	if strings.Contains(value, "payment") {
		return "payments"
	}
	return "gateway"
}

func inferErrorCode(statusCode int, requestPath string) string {
	switch {
	case statusCode >= 500:
		return "HTTP_500"
	case statusCode == http.StatusNotFound:
		return "HTTP_404"
	case statusCode == http.StatusForbidden:
		return "HTTP_403"
	case statusCode == http.StatusUnauthorized:
		return "HTTP_401"
	case requestPath == "/metrics":
		return "PROMETHEUS_SCRAPE"
	case requestPath == "/health":
		return "HEALTH_PROBE"
	default:
		return "HTTP_OK"
	}
}

func requestID(r *http.Request) string {
	if value := sanitizeRequestID(r.Header.Get("X-Request-Id")); value != "" {
		return value
	}
	return fmt.Sprintf("req-%d", reqIDSeq.Add(1))
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			if ip := parseIP(strings.TrimSpace(parts[0])); ip != "" {
				return ip
			}
		}
	}

	if ip := parseIP(strings.TrimSpace(r.RemoteAddr)); ip != "" {
		return ip
	}
	return "unknown"
}

func sanitizeRequestID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return -1
		}
	}, trimmed)

	if sanitized == "" {
		return ""
	}
	if len(sanitized) > 64 {
		return sanitized[:64]
	}
	return sanitized
}

func parseIP(value string) string {
	if value == "" {
		return ""
	}

	host := value
	if parsedHost, _, err := net.SplitHostPort(value); err == nil {
		host = parsedHost
	} else {
		host = strings.Trim(value, "[]")
	}

	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func roundMillis(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func buildIncidents(logs []LogEntry) []incident {
	type group struct {
		ID        string
		Service   string
		ErrorCode string
		Events    []LogEntry
	}

	groups := make(map[string]*group)
	for _, entry := range logs {
		if entry.Level != "error" {
			continue
		}

		key := entry.Service + ":" + entry.ErrorCode
		item, ok := groups[key]
		if !ok {
			item = &group{
				ID:        key,
				Service:   entry.Service,
				ErrorCode: entry.ErrorCode,
			}
			groups[key] = item
		}
		item.Events = append(item.Events, entry)
	}

	incidents := make([]incident, 0, len(groups))
	for _, item := range groups {
		slices.SortFunc(item.Events, func(a, b LogEntry) int {
			return strings.Compare(b.Timestamp, a.Timestamp)
		})

		score := scoreIncident(item.Events, logs)
		incidents = append(incidents, incident{
			ID:              item.ID,
			Service:         item.Service,
			ErrorCode:       item.ErrorCode,
			EventCount:      len(item.Events),
			LatestTimestamp: item.Events[0].Timestamp,
			Score:           score,
			Severity:        classifySeverity(score),
			Summary:         buildSummary(item.Service, item.ErrorCode, item.Events),
		})
	}

	slices.SortFunc(incidents, func(a, b incident) int {
		return b.Score - a.Score
	})

	return incidents
}

func scoreIncident(events []LogEntry, allLogs []LogEntry) int {
	if len(events) == 0 {
		return 0
	}

	frequencyWeight := min(len(events)*18, 45)
	serviceSet := make(map[string]struct{})
	noiseSet := make(map[string]struct{})
	latestEvent := time.Time{}

	for _, event := range events {
		serviceSet[event.Service] = struct{}{}
		ts, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err == nil && ts.After(latestEvent) {
			latestEvent = ts
		}
	}

	for _, log := range allLogs {
		noiseSet[log.Service] = struct{}{}
	}

	minutesSinceLast := 1
	if !latestEvent.IsZero() {
		minutesSinceLast = max(1, int(time.Since(latestEvent).Round(time.Minute)/time.Minute))
	}
	recencyWeight := max(10, 35-minutesSinceLast*2)

	score := frequencyWeight + recencyWeight + len(serviceSet)*10 + len(noiseSet)*3
	if score > 100 {
		return 100
	}
	return score
}

func classifySeverity(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 40:
		return "medium"
	default:
		return "low"
	}
}

func buildSummary(service, errorCode string, events []LogEntry) string {
	if len(events) == 0 {
		return ""
	}

	repeated := "seen once"
	if len(events) > 1 {
		repeated = fmt.Sprintf("repeated %d times", len(events))
	}

	timestamp := events[0].Timestamp
	if parsed, err := time.Parse(time.RFC3339Nano, events[0].Timestamp); err == nil {
		timestamp = parsed.Format("03:04 PM")
	}

	return fmt.Sprintf("%s is showing %s failures, %s, with the most recent event at %s.", service, errorCode, repeated, timestamp)
}
