package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultPort   = "8085"
	ScansDirName  = "scans"
	StaticDirName = "static"
	CookieName    = "auth_session"
)

type SaneDevice struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Type   string `json:"type"`
}

type ScanRequest struct {
	DPI        int    `json:"dpi"`
	Mode       string `json:"mode"`
	Format     string `json:"format"`
	CustomName string `json:"custom_name"`
	Device     string `json:"device"`
}

type MultiPageStartResponse struct {
	SessionID string `json:"session_id"`
}

type MultiPagePageRequest struct {
	SessionID string `json:"session_id"`
	DPI       int    `json:"dpi"`
	Mode      string `json:"mode"`
}

type MultiPageFinishRequest struct {
	SessionID  string   `json:"session_id"`
	CustomName string   `json:"custom_name"`
	PageOrder  []string `json:"page_order"`
}

type ScanItem struct {
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	SizeFormatted string    `json:"size_formatted"`
	ModTime       time.Time `json:"mod_time"`
	Format        string    `json:"format"`
	URL           string    `json:"url"`
}

var (
	baseDir          string
	scansDir         string
	staticDir        string
	sessionsDir      string
	validSessionRx   = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	validFileNameRx  = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)
	scanMutex        sync.Mutex
	cachedDevices    []SaneDevice
	cachedDefaultDev string
	lastDiscoverTime time.Time
	discoverMutex    sync.Mutex

	activeSessions   = make(map[string]time.Time)
	sessionMutex     sync.RWMutex
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func init() {
	var err error
	baseDir, err = os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	scansDir = filepath.Join(baseDir, ScansDirName)
	staticDir = filepath.Join(baseDir, StaticDirName)
	sessionsDir = filepath.Join(scansDir, ".temp_sessions")

	_ = os.MkdirAll(scansDir, 0755)
	_ = os.MkdirAll(sessionsDir, 0755)
	_ = os.MkdirAll(staticDir, 0755)
}

func main() {
	mux := http.NewServeMux()

	// Authentication Endpoints
	mux.HandleFunc("/api/auth/status", handleAuthStatus)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)

	// REST API Endpoints
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/scan/multipage/start", handleMultiPageStart)
	mux.HandleFunc("/api/scan/multipage/page", handleMultiPageAddPage)
	mux.HandleFunc("/api/scan/multipage/finish", handleMultiPageFinish)
	mux.HandleFunc("/api/scan/multipage/cancel", handleMultiPageCancel)
	mux.HandleFunc("/api/scans", handleScansList)
	mux.HandleFunc("/api/scans/", handleSingleScan)
	mux.HandleFunc("/api/printers", handlePrintersList)
	mux.HandleFunc("/api/print", handlePrintJob)
	mux.HandleFunc("/api/print/cancel", handleCancelPrintJob)

	// Static Assets
	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/", fileServer)

	// Security, Auth & Logging
	handler := securityMiddleware(loggingMiddleware(authMiddleware(mux)))

	startBackgroundCleaner()

	port := getEnv("PORT", DefaultPort)
	addr := ":" + port
	log.Printf("🚀 Scanner & Printer Web Hub running on port %s", port)
	log.Printf("📂 Scans directory: %s", scansDir)
	log.Printf("🖨️  Printing enabled: %v (ENABLE_PRINTING)", isPrintingEnabled())
	log.Printf("📠 Scanning enabled: %v (ENABLE_SCANNING)", isScanningEnabled())
	if isAuthRequired() {
		log.Printf("🔒 Web UI & API Authentication enabled (User: %s)", getAuthUsername())
	} else {
		log.Printf("🔓 Authentication disabled (Open network access)")
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 180 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stop
	log.Println("Received termination signal, shutting down gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped cleanly.")
}

func getAuthUsername() string {
	if u := os.Getenv("AUTH_USERNAME"); u != "" {
		return u
	}
	if u := os.Getenv("AUTH_USER"); u != "" {
		return u
	}
	return "admin"
}

func getAuthPassword() string {
	if p := os.Getenv("AUTH_PASSWORD"); p != "" {
		return p
	}
	return os.Getenv("AUTH_PASS")
}

func isPrintingEnabled() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_PRINTING")))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_PRINTER")))
	}
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_CUPS")))
	}
	if val == "false" || val == "0" || val == "no" || val == "off" || val == "disabled" {
		return false
	}
	return true
}

func isScanningEnabled() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_SCANNING")))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_SCANNER")))
	}
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_SANE")))
	}
	if val == "false" || val == "0" || val == "no" || val == "off" || val == "disabled" {
		return false
	}
	return true
}

func getDefaultScanFormat() string {
	f := strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_FORMAT")))
	if f == "" {
		f = strings.ToLower(strings.TrimSpace(os.Getenv("SCAN_FORMAT")))
	}
	if f == "png" || f == "jpg" || f == "jpeg" || f == "pdf" {
		if f == "jpeg" {
			return "jpg"
		}
		return f
	}
	return "pdf"
}

func isAuthRequired() bool {
	return getAuthPassword() != ""
}

func isValidSession(token string) bool {
	if token == "" {
		return false
	}
	sessionMutex.RLock()
	expiry, exists := activeSessions[token]
	sessionMutex.RUnlock()

	if !exists {
		return false
	}
	if time.Now().After(expiry) {
		sessionMutex.Lock()
		delete(activeSessions, token)
		sessionMutex.Unlock()
		return false
	}
	return true
}

func createSession() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)
	sessionMutex.Lock()
	activeSessions[token] = time.Now().Add(7 * 24 * time.Hour)
	sessionMutex.Unlock()
	return token, nil
}

// GET /api/auth/status
func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if !isAuthRequired() {
		jsonResponse(w, http.StatusOK, map[string]any{
			"auth_required": false,
			"logged_in":     true,
			"user":          "guest",
		})
		return
	}

	cookie, err := r.Cookie(CookieName)
	loggedIn := err == nil && isValidSession(cookie.Value)

	jsonResponse(w, http.StatusOK, map[string]any{
		"auth_required": true,
		"logged_in":     loggedIn,
		"user":          getAuthUsername(),
	})
}

// POST /api/login
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isAuthRequired() {
		jsonResponse(w, http.StatusOK, map[string]any{"success": true, "user": "guest"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	expectedUser := getAuthUsername()
	expectedPass := getAuthPassword()

	userMatch := subtle.ConstantTimeCompare([]byte(req.Username), []byte(expectedUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(req.Password), []byte(expectedPass)) == 1

	if !userMatch || !passMatch {
		jsonError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	token, err := createSession()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   7 * 24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"user":    expectedUser,
	})
}

// POST /api/logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(CookieName)
	if err == nil && cookie.Value != "" {
		sessionMutex.Lock()
		delete(activeSessions, cookie.Value)
		sessionMutex.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, http.StatusOK, map[string]any{"success": true})
}

// Optional Authentication Middleware
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAuthRequired() {
			next.ServeHTTP(w, r)
			return
		}

		// Allow public static assets and auth check/login endpoints
		path := r.URL.Path
		if path == "/api/login" || path == "/api/logout" || path == "/api/auth/status" ||
			path == "/styles.css" || path == "/app.js" || path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie
		if cookie, err := r.Cookie(CookieName); err == nil && isValidSession(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		// Also check HTTP Basic Auth for automated API/curl clients
		user, pass, ok := r.BasicAuth()
		if ok {
			expectedUser := getAuthUsername()
			expectedPass := getAuthPassword()
			if subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) == 1 &&
				subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		// If accessing API endpoints without auth, return 401 JSON
		if strings.HasPrefix(path, "/api/") {
			jsonError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		// For root HTML page, allow request so the frontend JS renders the clean login UI!
		next.ServeHTTP(w, r)
	})
}

// Security Middleware
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; script-src 'self' 'unsafe-inline'; connect-src 'self';")
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("[%s] %s (%s)", r.Method, r.URL.Path, time.Since(start))
		}
	})
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]any{
		"error":   true,
		"message": message,
	})
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Dynamic Discovery of Default CUPS Printer
func detectDefaultPrinter(ctx context.Context) string {
	if custom := os.Getenv("DEFAULT_PRINTER"); custom != "" {
		return custom
	}

	cmd := exec.CommandContext(ctx, "lpstat", "-d")
	out, err := cmd.CombinedOutput()
	if err == nil {
		str := strings.TrimSpace(string(out))
		if strings.Contains(str, ":") {
			parts := strings.SplitN(str, ":", 2)
			printerName := strings.TrimSpace(parts[1])
			if printerName != "" && printerName != "no system default destination" {
				return printerName
			}
		}
	}

	// Fallback to first available printer from lpstat -p
	cmdP := exec.CommandContext(ctx, "lpstat", "-p")
	outP, errP := cmdP.CombinedOutput()
	if errP == nil {
		for _, line := range strings.Split(string(outP), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "printer" {
				return fields[1]
			}
		}
	}

	return ""
}

// Cached Device Discovery for SANE Scanners with Auto-Discovery & Static IP support
func discoverScannerDevices(ctx context.Context, force bool) ([]SaneDevice, string) {
	discoverMutex.Lock()
	defer discoverMutex.Unlock()

	if !force && time.Since(lastDiscoverTime) < 60*time.Second && len(cachedDevices) > 0 {
		return cachedDevices, cachedDefaultDev
	}

	staticIP := os.Getenv("SCANNER_IP")
	if staticIP == "" {
		staticIP = os.Getenv("SCANNER_HOST")
	}
	customDefault := os.Getenv("DEFAULT_SCANNER")
	autoDiscover := strings.ToLower(os.Getenv("AUTO_DISCOVER")) != "false"

	var list []SaneDevice
	var defaultDev string

	// If a static scanner IP or host is configured, add it directly as high-priority
	if staticIP != "" {
		staticDeviceName := "epsonds:net:" + staticIP
		list = append(list, SaneDevice{
			Name:   staticDeviceName,
			Vendor: "Network",
			Model:  fmt.Sprintf("Scanner (%s)", staticIP),
			Type:   "flatbed scanner",
		})
		defaultDev = staticDeviceName
	}

	if customDefault != "" {
		defaultDev = customDefault
	}

	// Run dynamic network broadcast discovery if auto-discovery is enabled
	if autoDiscover {
		cmd := exec.CommandContext(ctx, "scanimage", "-f", "%d|%v|%m|%t%n")
		output, err := cmd.CombinedOutput()
		if err == nil && len(strings.TrimSpace(string(output))) > 0 {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				parts := strings.Split(strings.TrimSpace(line), "|")
				if len(parts) >= 4 {
					devName := parts[0]
					vendor := parts[1]
					model := parts[2]
					devType := parts[3]

					// Dynamically upgrade legacy epson2 network discovery to modern epsonds driver
					if strings.HasPrefix(devName, "epson2:net:") {
						ip := strings.TrimPrefix(devName, "epson2:net:")
						devName = "epsonds:net:" + ip
					}

					// Avoid duplicate entries
					exists := false
					for _, existing := range list {
						if existing.Name == devName {
							exists = true
							break
						}
					}

					if !exists {
						list = append(list, SaneDevice{
							Name:   devName,
							Vendor: vendor,
							Model:  model,
							Type:   devType,
						})
					}
				}
			}
		}
	}

	// Priority heuristics for default device if not explicitly set
	if defaultDev == "" && len(list) > 0 {
		for _, dev := range list {
			if strings.HasPrefix(strings.ToLower(dev.Name), "epsonds") {
				defaultDev = dev.Name
				break
			}
		}
		if defaultDev == "" {
			for _, dev := range list {
				if strings.HasPrefix(strings.ToLower(dev.Name), "airscan") {
					defaultDev = dev.Name
					break
				}
			}
		}
		if defaultDev == "" {
			defaultDev = list[0].Name
		}
	}

	cachedDevices = list
	cachedDefaultDev = defaultDev
	lastDiscoverTime = time.Now()

	return cachedDevices, cachedDefaultDev
}

// GET /api/status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	printingEnabled := isPrintingEnabled()
	scanningEnabled := isScanningEnabled()

	var devices []SaneDevice
	var defaultDev string
	var scannerFound bool

	if scanningEnabled {
		force := r.URL.Query().Get("refresh") == "true"
		devices, defaultDev = discoverScannerDevices(ctx, force)
		scannerFound = len(devices) > 0 || defaultDev != ""
	}

	var defaultPrinter string
	var cupsOutputStr string
	var printerFound bool
	var accepting bool
	var activeJobs int

	if printingEnabled {
		defaultPrinter = detectDefaultPrinter(ctx)

		cmdCups := exec.CommandContext(ctx, "lpstat", "-p", "-d")
		cupsOut, _ := cmdCups.CombinedOutput()
		cupsOutputStr = string(cupsOut)

		printerFound = defaultPrinter != "" || strings.Contains(cupsOutputStr, "printer")
		accepting = strings.Contains(cupsOutputStr, "enabled") || strings.Contains(cupsOutputStr, "accepting") || strings.Contains(cupsOutputStr, "idle")

		cmdJobs := exec.CommandContext(ctx, "lpstat", "-o")
		jobsOut, _ := cmdJobs.CombinedOutput()
		if len(strings.TrimSpace(string(jobsOut))) > 0 {
			activeJobs = len(strings.Split(strings.TrimSpace(string(jobsOut)), "\n"))
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"features": map[string]bool{
			"scanning": scanningEnabled,
			"printing": printingEnabled,
		},
		"scanner": map[string]any{
			"enabled":        scanningEnabled,
			"online":         scannerFound,
			"default_device": defaultDev,
			"default_format": getDefaultScanFormat(),
			"devices":        devices,
		},
		"printer": map[string]any{
			"enabled":     printingEnabled,
			"online":      printerFound,
			"name":        defaultPrinter,
			"accepting":   accepting,
			"raw_status":  cupsOutputStr,
			"active_jobs": activeJobs,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// Native Scanner Execution with Dynamic Fallback
func executeScan(ctx context.Context, device string, dpi int, mode string, format string, targetPath string) error {
	var scanFormat string
	switch format {
	case "jpg", "jpeg":
		scanFormat = "jpeg"
	case "pdf":
		scanFormat = "png"
	default:
		scanFormat = "png"
	}

	targetDev := device
	if targetDev == "" {
		targetDev = cachedDefaultDev
	}
	if targetDev == "" {
		_, targetDev = discoverScannerDevices(ctx, false)
	}

	var scanOutFile string
	if format == "pdf" {
		tempPNG, err := os.CreateTemp("", "scan_temp_*.png")
		if err != nil {
			return err
		}
		scanOutFile = tempPNG.Name()
		_ = tempPNG.Close()
		defer os.Remove(scanOutFile)
	} else {
		scanOutFile = targetPath
	}

	args := []string{
		"--format", scanFormat,
		"--resolution", strconv.Itoa(dpi),
		"-o", scanOutFile,
	}
	if targetDev != "" {
		args = append([]string{"-d", targetDev}, args...)
	}

	if mode == "Gray" {
		args = append(args, "--mode", "Gray")
	} else if mode == "Lineart" {
		args = append(args, "--mode", "Lineart")
	} else {
		args = append(args, "--mode", "Color")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.CommandContext(ctx, "scanimage", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			lastErr = fmt.Errorf("%v: %s", err, stderr.String())
			time.Sleep(2 * time.Second)
			continue
		}

		if format == "pdf" {
			pngData, err := os.ReadFile(scanOutFile)
			if err != nil {
				return err
			}
			return convertImagesToPDFInPureGo([][]byte{pngData}, dpi, targetPath)
		}
		return nil
	}

	return lastErr
}

// POST /api/scan
func handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isScanningEnabled() {
		jsonError(w, http.StatusForbidden, "Scanning functionality is disabled by server configuration (ENABLE_SCANNING=false)")
		return
	}

	if !scanMutex.TryLock() {
		jsonError(w, http.StatusConflict, "Scanner is currently busy. Please wait for the ongoing scan to complete.")
		return
	}
	defer scanMutex.Unlock()

	var req ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON request body")
		return
	}

	switch req.DPI {
	case 75, 100, 150, 200, 300, 600:
	default:
		req.DPI = 300
	}

	switch req.Mode {
	case "Color", "Gray", "Lineart":
	default:
		req.Mode = "Color"
	}

	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	switch req.Format {
	case "png", "jpg", "jpeg", "pdf":
		if req.Format == "jpeg" {
			req.Format = "jpg"
		}
	default:
		req.Format = "png"
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	var safeCustom string
	if req.CustomName != "" {
		safeCustom = sanitizeName(req.CustomName)
	}

	var filename string
	if safeCustom != "" {
		filename = fmt.Sprintf("scan_%s_%s.%s", timestamp, safeCustom, req.Format)
	} else {
		filename = fmt.Sprintf("scan_%s.%s", timestamp, req.Format)
	}

	outputPath := filepath.Join(scansDir, filename)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := executeScan(ctx, req.Device, req.DPI, req.Mode, req.Format, outputPath); err != nil {
		log.Printf("Scan error: %v", err)
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Scanning failed: %v", err))
		return
	}

	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Scanned file verification failed")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"success":        true,
		"filename":       filename,
		"size":           fileInfo.Size(),
		"size_formatted": formatFileSize(fileInfo.Size()),
		"url":            "/api/scans/" + filename,
	})
}

// POST /api/scan/multipage/start
func handleMultiPageStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isScanningEnabled() {
		jsonError(w, http.StatusForbidden, "Scanning functionality is disabled by server configuration (ENABLE_SCANNING=false)")
		return
	}

	sessionID := fmt.Sprintf("session_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixNano()%1000)
	sessionPath := filepath.Join(sessionsDir, sessionID)

	if err := os.MkdirAll(sessionPath, 0755); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to initialize session directory")
		return
	}

	jsonResponse(w, http.StatusOK, MultiPageStartResponse{
		SessionID: sessionID,
	})
}

// POST /api/scan/multipage/page
func handleMultiPageAddPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isScanningEnabled() {
		jsonError(w, http.StatusForbidden, "Scanning functionality is disabled by server configuration (ENABLE_SCANNING=false)")
		return
	}

	if !scanMutex.TryLock() {
		jsonError(w, http.StatusConflict, "Scanner is currently busy. Please wait for the ongoing scan to complete.")
		return
	}
	defer scanMutex.Unlock()

	var req MultiPagePageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if !validSessionRx.MatchString(req.SessionID) {
		jsonError(w, http.StatusBadRequest, "Invalid session ID format")
		return
	}

	sessionPath := filepath.Join(sessionsDir, req.SessionID)
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		jsonError(w, http.StatusNotFound, "Session not found or expired")
		return
	}

	entries, _ := os.ReadDir(sessionPath)
	pageIdx := len(entries) + 1
	pageFilename := fmt.Sprintf("page_%03d.png", pageIdx)
	pagePath := filepath.Join(sessionPath, pageFilename)

	switch req.DPI {
	case 75, 100, 150, 200, 300, 600:
	default:
		req.DPI = 300
	}
	switch req.Mode {
	case "Color", "Gray", "Lineart":
	default:
		req.Mode = "Color"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := executeScan(ctx, "", req.DPI, req.Mode, "png", pagePath); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Page scan failed: %v", err))
		return
	}

	fileInfo, _ := os.Stat(pagePath)

	jsonResponse(w, http.StatusOK, map[string]any{
		"success":        true,
		"page_number":    pageIdx,
		"page_filename":  pageFilename,
		"total_pages":    pageIdx,
		"size":           fileInfo.Size(),
		"size_formatted": formatFileSize(fileInfo.Size()),
		"url":            fmt.Sprintf("/api/scans/.temp_sessions/%s/%s", req.SessionID, pageFilename),
	})
}

// POST /api/scan/multipage/finish
func handleMultiPageFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req MultiPageFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if !validSessionRx.MatchString(req.SessionID) {
		jsonError(w, http.StatusBadRequest, "Invalid session ID")
		return
	}

	sessionPath := filepath.Join(sessionsDir, req.SessionID)
	entries, err := os.ReadDir(sessionPath)
	if err != nil || len(entries) == 0 {
		jsonError(w, http.StatusBadRequest, "No scanned pages found in this session")
		return
	}

	var imageDatas [][]byte
	if len(req.PageOrder) > 0 {
		for _, p := range req.PageOrder {
			cleanP := filepath.Base(p)
			fullP := filepath.Join(sessionPath, cleanP)
			data, err := os.ReadFile(fullP)
			if err == nil {
				imageDatas = append(imageDatas, data)
			}
		}
	} else {
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".png") || strings.HasSuffix(strings.ToLower(e.Name()), ".jpg") {
				data, err := os.ReadFile(filepath.Join(sessionPath, e.Name()))
				if err == nil {
					imageDatas = append(imageDatas, data)
				}
			}
		}
	}

	if len(imageDatas) == 0 {
		jsonError(w, http.StatusBadRequest, "No valid image pages to merge")
		return
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	var safeCustom string
	if req.CustomName != "" {
		safeCustom = sanitizeName(req.CustomName)
	}

	var outputFilename string
	if safeCustom != "" {
		outputFilename = fmt.Sprintf("doc_%s_%s.pdf", timestamp, safeCustom)
	} else {
		outputFilename = fmt.Sprintf("doc_%s.pdf", timestamp)
	}

	outputPDFPath := filepath.Join(scansDir, outputFilename)

	if err := convertImagesToPDFInPureGo(imageDatas, 300, outputPDFPath); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("PDF compilation failed: %v", err))
		return
	}

	_ = os.RemoveAll(sessionPath)

	fileInfo, _ := os.Stat(outputPDFPath)
	jsonResponse(w, http.StatusOK, map[string]any{
		"success":        true,
		"filename":       outputFilename,
		"pages_count":    len(imageDatas),
		"size":           fileInfo.Size(),
		"size_formatted": formatFileSize(fileInfo.Size()),
		"url":            "/api/scans/" + outputFilename,
	})
}

// POST /api/scan/multipage/cancel
func handleMultiPageCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && validSessionRx.MatchString(req.SessionID) {
		sessionPath := filepath.Join(sessionsDir, req.SessionID)
		_ = os.RemoveAll(sessionPath)
	}

	jsonResponse(w, http.StatusOK, map[string]any{"success": true})
}

// Pure Go Image-to-PDF Converter
func convertImagesToPDFInPureGo(imageDatas [][]byte, dpi int, outputPath string) error {
	if len(imageDatas) == 0 {
		return fmt.Errorf("no images provided")
	}

	numPages := len(imageDatas)
	var fullPDF bytes.Buffer
	fullPDF.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	var offsets []int
	offsets = append(offsets, 0)

	var kids []string
	for i := 0; i < numPages; i++ {
		pageObjID := 5 + 3*i
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjID))
	}

	offsets = append(offsets, fullPDF.Len())
	fullPDF.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	offsets = append(offsets, fullPDF.Len())
	fullPDF.WriteString(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n", strings.Join(kids, " "), numPages))

	for i, data := range imageDatas {
		imgObjID := 3 + 3*i
		cntObjID := 4 + 3*i
		pageObjID := 5 + 3*i

		img, _, err := image.Decode(bytes.NewReader(data))
		var imgW, imgH int
		var rgbStream []byte
		if err == nil {
			bounds := img.Bounds()
			imgW = bounds.Dx()
			imgH = bounds.Dy()
			rgbStream = make([]byte, imgW*imgH*3)
			idx := 0
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					rgbStream[idx] = byte(r >> 8)
					rgbStream[idx+1] = byte(g >> 8)
					rgbStream[idx+2] = byte(b >> 8)
					idx += 3
				}
			}
		} else {
			imgW = 2550
			imgH = 3300
			rgbStream = make([]byte, imgW*imgH*3)
		}

		widthPt := float64(imgW) * 72.0 / float64(dpi)
		heightPt := float64(imgH) * 72.0 / float64(dpi)

		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		_, _ = zw.Write(rgbStream)
		_ = zw.Close()
		compressedData := compressed.Bytes()

		offsets = append(offsets, fullPDF.Len())
		fullPDF.WriteString(fmt.Sprintf("%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n", imgObjID, imgW, imgH, len(compressedData)))
		fullPDF.Write(compressedData)
		fullPDF.WriteString("\nendstream\nendobj\n")

		cntStr := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im%d Do Q", widthPt, heightPt, i+1)
		offsets = append(offsets, fullPDF.Len())
		fullPDF.WriteString(fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", cntObjID, len(cntStr), cntStr))

		offsets = append(offsets, fullPDF.Len())
		fullPDF.WriteString(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Contents %d 0 R /Resources << /XObject << /Im%d %d 0 R >> >> >>\nendobj\n", pageObjID, widthPt, heightPt, cntObjID, i+1, imgObjID))
	}

	xrefStart := fullPDF.Len()
	fullPDF.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(offsets)))
	for idx := 1; idx < len(offsets); idx++ {
		fullPDF.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[idx]))
	}

	fullPDF.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefStart))

	return os.WriteFile(outputPath, fullPDF.Bytes(), 0644)
}

// Background cleanup worker for abandoned temp sessions & expired auth sessions
func startBackgroundCleaner() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			// 1. Clean expired auth sessions
			sessionMutex.Lock()
			now := time.Now()
			for token, expiry := range activeSessions {
				if now.After(expiry) {
					delete(activeSessions, token)
				}
			}
			sessionMutex.Unlock()

			// 2. Clean abandoned temporary multi-page scan sessions older than 2 hours
			entries, err := os.ReadDir(sessionsDir)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						info, err := entry.Info()
						if err == nil && time.Since(info.ModTime()) > 2*time.Hour {
							_ = os.RemoveAll(filepath.Join(sessionsDir, entry.Name()))
						}
					}
				}
			}
		}
	}()
}

// GET /api/scans
func handleScansList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	entries, err := os.ReadDir(scansDir)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read scans directory")
		return
	}

	var items []ScanItem
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), ".")
		items = append(items, ScanItem{
			Name:          entry.Name(),
			Size:          info.Size(),
			SizeFormatted: formatFileSize(info.Size()),
			ModTime:       info.ModTime(),
			Format:        ext,
			URL:           "/api/scans/" + entry.Name(),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ModTime.After(items[j].ModTime)
	})

	jsonResponse(w, http.StatusOK, map[string]any{
		"scans": items,
		"count": len(items),
	})
}

// GET / DELETE /api/scans/{filename}
func handleSingleScan(w http.ResponseWriter, r *http.Request) {
	subPath := strings.TrimPrefix(r.URL.Path, "/api/scans/")
	if subPath == "" {
		jsonError(w, http.StatusBadRequest, "Filename required")
		return
	}

	if strings.HasPrefix(subPath, ".temp_sessions/") {
		parts := strings.Split(subPath, "/")
		if len(parts) == 3 && validSessionRx.MatchString(parts[1]) && validFileNameRx.MatchString(parts[2]) {
			filePath := filepath.Join(sessionsDir, parts[1], parts[2])
			http.ServeFile(w, r, filePath)
			return
		}
		jsonError(w, http.StatusBadRequest, "Invalid temporary path")
		return
	}

	safeFilename := filepath.Base(subPath)
	if safeFilename == "." || safeFilename == "/" || !validFileNameRx.MatchString(safeFilename) {
		jsonError(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	targetPath := filepath.Join(scansDir, safeFilename)

	rel, err := filepath.Rel(scansDir, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		jsonError(w, http.StatusForbidden, "Access denied")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			jsonError(w, http.StatusNotFound, "File not found")
			return
		}

		if r.URL.Query().Get("download") == "true" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeFilename))
		}
		http.ServeFile(w, r, targetPath)

	case http.MethodDelete:
		if err := os.Remove(targetPath); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to delete file")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "File deleted successfully",
		})

	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// GET /api/printers
func handlePrintersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isPrintingEnabled() {
		jsonResponse(w, http.StatusOK, map[string]any{
			"enabled":         false,
			"default_printer": "",
			"status_text":     "Printing is disabled via server configuration (ENABLE_PRINTING=false)",
			"jobs":            []string{},
		})
		return
	}

	defaultPrinter := detectDefaultPrinter(r.Context())

	cmdStat := exec.Command("lpstat", "-p", "-d")
	outStat, _ := cmdStat.CombinedOutput()

	cmdJobs := exec.Command("lpstat", "-o")
	outJobs, _ := cmdJobs.CombinedOutput()

	var jobsList []string
	if len(strings.TrimSpace(string(outJobs))) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(string(outJobs)), "\n") {
			if strings.TrimSpace(line) != "" {
				jobsList = append(jobsList, strings.TrimSpace(line))
			}
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"enabled":         true,
		"default_printer": defaultPrinter,
		"status_text":     string(outStat),
		"jobs":            jobsList,
	})
}

// POST /api/print
func handlePrintJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isPrintingEnabled() {
		jsonError(w, http.StatusForbidden, "Printing functionality is disabled by server configuration (ENABLE_PRINTING=false)")
		return
	}

	// Limit upload size to 50MB to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	contentType := r.Header.Get("Content-Type")

	var targetFilePath string
	var cleanupTemp bool
	var copies = 1

	if strings.Contains(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(25 << 20); err != nil {
			jsonError(w, http.StatusBadRequest, "File too large or invalid form data")
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "No file provided in form")
			return
		}
		defer file.Close()

		if copiesStr := r.FormValue("copies"); copiesStr != "" {
			if c, err := strconv.Atoi(copiesStr); err == nil && c >= 1 && c <= 20 {
				copies = c
			}
		}

		ext := strings.ToLower(filepath.Ext(header.Filename))
		tempFile, err := os.CreateTemp("", "print_upload_*"+ext)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to create temporary print file")
			return
		}
		defer tempFile.Close()

		if _, err := io.Copy(tempFile, file); err != nil {
			_ = os.Remove(tempFile.Name())
			jsonError(w, http.StatusInternalServerError, "Failed to save print file")
			return
		}

		targetFilePath = tempFile.Name()
		cleanupTemp = true

	} else {
		var req struct {
			ScanFilename string `json:"scan_filename"`
			Copies       int    `json:"copies"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "Invalid JSON payload")
			return
		}

		safeName := filepath.Base(req.ScanFilename)
		targetFilePath = filepath.Join(scansDir, safeName)
		if _, err := os.Stat(targetFilePath); os.IsNotExist(err) {
			jsonError(w, http.StatusNotFound, "Scan file not found")
			return
		}

		if req.Copies >= 1 && req.Copies <= 20 {
			copies = req.Copies
		}
	}

	if cleanupTemp {
		defer os.Remove(targetFilePath)
	}

	printer := detectDefaultPrinter(r.Context())

	args := []string{
		"-n", strconv.Itoa(copies),
		targetFilePath,
	}
	if printer != "" {
		args = append([]string{"-d", printer}, args...)
	}

	cmd := exec.Command("lp", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Print error: %v, output: %s", err, string(output))
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Print job failed: %s", strings.TrimSpace(string(output))))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"message": strings.TrimSpace(string(output)),
	})
}

// POST /api/print/cancel
func handleCancelPrintJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !isPrintingEnabled() {
		jsonError(w, http.StatusForbidden, "Printing functionality is disabled by server configuration (ENABLE_PRINTING=false)")
		return
	}

	var req struct {
		JobID string `json:"job_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var cmd *exec.Cmd
	if req.JobID != "" && validFileNameRx.MatchString(req.JobID) {
		cmd = exec.Command("cancel", req.JobID)
	} else {
		printer := detectDefaultPrinter(r.Context())
		if printer != "" {
			cmd = exec.Command("cancel", "-a", printer)
		} else {
			cmd = exec.Command("cancel", "-a")
		}
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("Cancel failed: %s", strings.TrimSpace(string(out))))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Print job(s) cancelled",
	})
}

func sanitizeName(input string) string {
	var clean strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			clean.WriteRune(r)
		} else if r == ' ' {
			clean.WriteRune('_')
		}
	}
	res := clean.String()
	if len(res) > 50 {
		res = res[:50]
	}
	return res
}
