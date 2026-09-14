package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/KyleYu2024/mosctl/internal/service"
	"github.com/KyleYu2024/mosctl/internal/version"
)

const (
	defaultListen  = ":9090"
	maxRuleSize    = 2 << 20
	sessionTTL     = 30 * 24 * time.Hour
	sessionKeyFile = ".web_session_key"
)

//go:embed static/*
var staticFiles embed.FS

type ruleSpec struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Description string `json:"description"`
}

var editableRules = []ruleSpec{
	{ID: "force-cn", Name: "强制国内", Filename: "force-cn.txt", Description: "始终使用国内 DNS 解析的域名"},
	{ID: "force-nocn", Name: "强制国外", Filename: "force-nocn.txt", Description: "始终使用国外 DNS 解析的域名"},
	{ID: "user-iot", Name: "IoT 设备", Filename: "user_iot.txt", Description: "指定直连的设备 IP 或 CIDR"},
	{ID: "hosts", Name: "Hosts", Filename: "hosts.txt", Description: "自定义域名与 IP 映射"},
}

type Options struct {
	RuleDir string
	Restart func() error
	Logger  *log.Logger
}

type Server struct {
	ruleDir    string
	restart    func() error
	logger     *log.Logger
	username   string
	password   string
	sessionKey []byte

	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

type loginAttempt struct {
	count   int
	resetAt time.Time
}

func New(opts Options) (*Server, error) {
	username := strings.TrimSpace(os.Getenv("USERNAME"))
	password := os.Getenv("PASSWORD")
	if username == "" || password == "" {
		return nil, errors.New("USERNAME 和 PASSWORD 必须同时配置")
	}
	if opts.RuleDir == "" {
		opts.RuleDir = "/etc/mosdns/rules"
	}
	if opts.Restart == nil {
		opts.Restart = service.RestartService
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	sessionKey, err := loadOrCreateSessionKey(filepath.Dir(opts.RuleDir))
	if err != nil {
		return nil, fmt.Errorf("无法初始化 Web 会话密钥: %w", err)
	}
	credentialMAC := hmac.New(sha256.New, sessionKey)
	credentialMAC.Write([]byte(username + "\x00" + password))
	sessionKey = credentialMAC.Sum(nil)
	return &Server{
		ruleDir: opts.RuleDir, restart: opts.Restart, logger: opts.Logger,
		username: username, password: password, sessionKey: sessionKey,
		attempts: make(map[string]*loginAttempt),
	}, nil
}

func Run(ctx context.Context) {
	srv, err := New(Options{})
	if err != nil {
		log.Printf("⚠️ Web 管理页未启动: %v", err)
		return
	}
	listen := strings.TrimSpace(os.Getenv("WEB_LISTEN"))
	if listen == "" {
		listen = defaultListen
	}
	httpServer := &http.Server{
		Addr: listen, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("🌐 MosCtl Web 管理页已启动: %s", listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("❌ Web 管理页退出: %v", err)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.serveApp)
	mux.HandleFunc("GET /manifest.webmanifest", s.servePublicAsset)
	mux.HandleFunc("GET /sw.js", s.servePublicAsset)
	mux.HandleFunc("GET /assets/", s.servePublicAsset)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.logout))
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("GET /api/rules", s.requireAuth(s.listRules))
	mux.HandleFunc("GET /api/rules/{id}", s.requireAuth(s.getRule))
	mux.HandleFunc("PUT /api/rules/{id}", s.requireAuth(s.saveRule))
	return s.securityHeaders(mux)
}

func (s *Server) servePublicAsset(w http.ResponseWriter, r *http.Request) {
	assets := map[string]struct {
		name        string
		contentType string
	}{
		"/manifest.webmanifest":         {"manifest.webmanifest", "application/manifest+json"},
		"/sw.js":                        {"sw.js", "application/javascript; charset=utf-8"},
		"/assets/logout.svg":            {"logout.svg", "image/svg+xml"},
		"/assets/icon-192.png":          {"icon-192.png", "image/png"},
		"/assets/icon-512.png":          {"icon-512.png", "image/png"},
		"/assets/icon-maskable-512.png": {"icon-maskable-512.png", "image/png"},
		"/assets/apple-touch-icon.png":  {"apple-touch-icon.png", "image/png"},
	}
	asset, ok := assets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/" + asset.name)
	if err != nil {
		http.Error(w, "unable to load asset", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", asset.contentType)
	if r.URL.Path == "/sw.js" {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	w.Write(data)
}

func (s *Server) serveApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "unable to load app", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(string(data), "{{VERSION}}", version.Current)
	w.Write([]byte(page))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "请求来源无效")
		return
	}
	ip := clientIP(r)
	if !s.allowLogin(ip) {
		writeError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	var req struct{ Username, Password string }
	if err := decodeJSON(r, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "请输入用户名和密码")
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(req.Username), []byte(s.username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.password)) == 1
	if !userOK || !passOK {
		s.recordFailedLogin(ip)
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "用户名或密码不正确")
		return
	}
	token, err := s.newSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.mu.Lock()
	delete(s.attempts, ip)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "mosctl_session", Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: requestIsHTTPS(r), MaxAge: int(sessionTTL.Seconds()),
		Expires: time.Now().Add(sessionTTL),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": s.username})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "mosctl_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		writeError(w, http.StatusUnauthorized, "未登录")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": s.username})
}

func (s *Server) listRules(w http.ResponseWriter, _ *http.Request) {
	type summary struct {
		ruleSpec
		Entries int    `json:"entries"`
		Updated string `json:"updated"`
	}
	result := make([]summary, 0, len(editableRules))
	for _, spec := range editableRules {
		content, info, _ := s.readRule(spec)
		updated := "尚未修改"
		if info != nil {
			updated = info.ModTime().Format(time.RFC3339)
		}
		result = append(result, summary{ruleSpec: spec, Entries: countEntries(content), Updated: updated})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": result})
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	spec, ok := findRule(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "规则集不存在")
		return
	}
	content, info, err := s.readRule(spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取规则")
		return
	}
	updated := ""
	if info != nil {
		updated = info.ModTime().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": spec, "content": content, "entries": countEntries(content), "updated": updated})
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "请求来源无效")
		return
	}
	spec, ok := findRule(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "规则集不存在")
		return
	}
	var req struct {
		Content string `json:"content"`
		Apply   bool   `json:"apply"`
	}
	if err := decodeJSON(r, &req, maxRuleSize+4096); err != nil {
		writeError(w, http.StatusBadRequest, "规则内容无效或过大")
		return
	}
	content, err := normalizeContent(req.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(s.ruleDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建规则目录")
		return
	}
	path := filepath.Join(s.ruleDir, spec.Filename)
	tmp, err := os.CreateTemp(s.ruleDir, ".mosctl-rule-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法保存规则")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err == nil {
		_, err = io.WriteString(tmp, content)
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		s.logger.Printf("保存规则 %s 失败: %v", spec.Filename, err)
		writeError(w, http.StatusInternalServerError, "无法保存规则")
		return
	}
	message := "规则已保存"
	if req.Apply {
		if err := s.restart(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "规则已保存，但 MosDNS 重载失败", "saved": true})
			return
		}
		message = "规则已保存并应用"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message, "entries": countEntries(content)})
}

func (s *Server) readRule(spec ruleSpec) (string, os.FileInfo, error) {
	path := filepath.Join(s.ruleDir, spec.Filename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	info, _ := os.Stat(path)
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content, info, nil
}

func findRule(id string) (ruleSpec, bool) {
	for _, spec := range editableRules {
		if spec.ID == id {
			return spec, true
		}
	}
	return ruleSpec{}, false
}

func normalizeContent(content string) (string, error) {
	if len(content) > maxRuleSize {
		return "", fmt.Errorf("单个规则文件不能超过 2 MB")
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return "", fmt.Errorf("规则必须是有效的 UTF-8 文本")
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	content = strings.TrimSpace(strings.Join(lines, "\n"))
	if content != "" {
		content += "\n"
	}
	return content, nil
}

func countEntries(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			writeError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		next(w, r)
	}
}

func (s *Server) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie("mosctl_session")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || len(parts[1]) != 32 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return false
	}
	signature, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.sessionKey)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	return hmac.Equal(signature, mac.Sum(nil))
}

func (s *Server) newSessionToken() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	payload := strconv.FormatInt(time.Now().Add(sessionTTL).Unix(), 10) + "." + hex.EncodeToString(randomBytes)
	mac := hmac.New(sha256.New, s.sessionKey)
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func loadOrCreateSessionKey(configDir string) ([]byte, error) {
	keyPath := filepath.Join(configDir, sessionKeyFile)
	if key, err := os.ReadFile(keyPath); err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("会话密钥长度无效")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(configDir, ".web-session-key-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(key)
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, keyPath)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Server) allowLogin(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.attempts[ip]
	if !ok || time.Now().After(a.resetAt) {
		delete(s.attempts, ip)
		return true
	}
	return a.count < 5
}

func (s *Server) recordFailedLogin(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.attempts[ip]
	if !ok || time.Now().After(a.resetAt) {
		a = &loginAttempt{resetAt: time.Now().Add(5 * time.Minute)}
		s.attempts[ip] = a
	}
	a.count++
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	expected := "http://" + r.Host
	if requestIsHTTPS(r) {
		expected = "https://" + r.Host
	}
	return origin == expected
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(r *http.Request, dst any, limit int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
