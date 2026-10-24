package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

// Config holds application settings loaded from environment or .env
type Config struct {
	AuthKey    string
	Domain     string
	LogPath    string
	UpdateTime time.Duration
}

// Logger handles timestamped logging to both file and standard streams.
type Logger struct {
	mu       sync.Mutex
	filePath string
}

func NewLogger(filePath string) *Logger {
	return &Logger{
		filePath: filePath,
	}
}

func (l *Logger) writeToFile(prefix, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formatted := fmt.Sprintf("[%s] [%s] %s\n", timestamp, prefix, msg)

	// Append to log file
	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		_, _ = f.WriteString(formatted)
	}

	// Also output to console
	if prefix == "ERROR" {
		os.Stderr.WriteString(formatted)
	} else {
		os.Stdout.WriteString(formatted)
	}
}

func (l *Logger) LogSuccess(msg string) {
	l.writeToFile("SUCCESS", msg)
}

func (l *Logger) LogError(msg string) {
	l.writeToFile("ERROR", msg)
}

func (l *Logger) LogInfo(msg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("[%s] [INFO] %s\n", timestamp, msg)
}

// Cloudflare API data structures
type CloudflareResponse[T any] struct {
	Success  bool      `json:"success"`
	Errors   []CFError `json:"errors"`
	Messages []string  `json:"messages"`
	Result   T         `json:"result"`
}

type CFError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e CFError) String() string {
	return fmt.Sprintf("code %d: %s", e.Code, e.Message)
}

func formatCFErrors(errors []CFError) string {
	if len(errors) == 0 {
		return "unknown Cloudflare error"
	}
	var errStrs []string
	for _, e := range errors {
		errStrs = append(errStrs, e.String())
	}
	return strings.Join(errStrs, "; ")
}

type ZoneResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DNSRecordResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

type DNSUpdateRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type IPInfoResponse struct {
	IP string `json:"ip"`
}

// loadConfig reads .env if present and environment variables
func loadConfig() (*Config, error) {
	// Attempt to load .env; ignore error if not found since env vars may be set externally
	_ = godotenv.Load()

	authKey := strings.TrimSpace(os.Getenv("AUTH_KEY"))
	if authKey == "" {
		return nil, fmt.Errorf("AUTH_KEY is required in .env or environment")
	}

	domain := strings.TrimSpace(os.Getenv("DOMAIN"))
	if domain == "" {
		return nil, fmt.Errorf("DOMAIN is required in .env or environment")
	}

	logPath := strings.TrimSpace(os.Getenv("LOG"))
	if logPath == "" {
		logPath = "cloudflareUpdateLog.txt"
	}

	updateTimeSec := 1200
	updateTimeStr := strings.TrimSpace(os.Getenv("UPDATE_TIME"))
	if updateTimeStr != "" {
		if val, err := strconv.Atoi(updateTimeStr); err == nil && val > 0 {
			updateTimeSec = val
		}
	}

	return &Config{
		AuthKey:    authKey,
		Domain:     domain,
		LogPath:    logPath,
		UpdateTime: time.Duration(updateTimeSec) * time.Second,
	}, nil
}

// findZoneIdentifier finds the Cloudflare zone identifier for the given domain.
// It searches candidate domains (e.g. for sub.domain.com it checks sub.domain.com then domain.com)
// and falls back to listing all authorized zones to find the longest matching suffix.
func findZoneIdentifier(client *http.Client, authKey, domain string) (string, string, error) {
	// Candidate search by name
	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts)-1; i++ {
		candidate := strings.Join(parts[i:], ".")
		url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones?name=%s", candidate)

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Authorization", "Bearer "+authKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var cfResp CloudflareResponse[[]ZoneResult]
		if err := json.Unmarshal(body, &cfResp); err == nil && cfResp.Success && len(cfResp.Result) > 0 {
			return cfResp.Result[0].ID, cfResp.Result[0].Name, nil
		}
	}

	// Fallback: list authorized zones
	listURL := "https://api.cloudflare.com/client/v4/zones"
	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+authKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zone query request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed reading zone response: %w", err)
	}

	var cfResp CloudflareResponse[[]ZoneResult]
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return "", "", fmt.Errorf("failed parsing zone response: %w (body: %s)", err, string(body))
	}

	if !cfResp.Success {
		return "", "", fmt.Errorf("Cloudflare error fetching zones: %s", formatCFErrors(cfResp.Errors))
	}

	// Match zone with the longest name that is a suffix of the target domain
	var bestMatch *ZoneResult
	for _, zone := range cfResp.Result {
		if domain == zone.Name || strings.HasSuffix(domain, "."+zone.Name) {
			if bestMatch == nil || len(zone.Name) > len(bestMatch.Name) {
				zoneCopy := zone
				bestMatch = &zoneCopy
			}
		}
	}

	if bestMatch != nil {
		return bestMatch.ID, bestMatch.Name, nil
	}

	return "", "", fmt.Errorf("no matching zone found for domain %q", domain)
}

// findDNSRecord queries Cloudflare for the A record matching the specified domain.
func findDNSRecord(client *http.Client, authKey, zoneID, domain string) (*DNSRecordResult, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=A&name=%s", zoneID, domain)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+authKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DNS record query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading DNS record response: %w", err)
	}

	var cfResp CloudflareResponse[[]DNSRecordResult]
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return nil, fmt.Errorf("failed parsing DNS record response: %w (body: %s)", err, string(body))
	}

	if !cfResp.Success {
		return nil, fmt.Errorf("Cloudflare error querying DNS records: %s", formatCFErrors(cfResp.Errors))
	}

	for _, record := range cfResp.Result {
		if record.Name == domain && record.Type == "A" {
			return &record, nil
		}
	}

	return nil, fmt.Errorf("no A record found for domain %q in zone %s", domain, zoneID)
}

// fetchPublicIP queries ipinfo.io to determine current public IPv4.
func fetchPublicIP(client *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://ipinfo.io", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch public IP from ipinfo.io: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ipinfo.io returned HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed reading ipinfo.io response: %w", err)
	}

	var ipInfo IPInfoResponse
	if err := json.Unmarshal(body, &ipInfo); err != nil {
		return "", fmt.Errorf("failed parsing ipinfo.io JSON: %w (body: %s)", err, string(body))
	}

	ipStr := strings.TrimSpace(ipInfo.IP)
	parsed := net.ParseIP(ipStr)
	if parsed == nil || parsed.To4() == nil {
		return "", fmt.Errorf("invalid IPv4 address received from ipinfo.io: %q", ipStr)
	}

	return ipStr, nil
}

// updateDNSRecord executes a PUT request to update the Cloudflare DNS record.
func updateDNSRecord(client *http.Client, authKey, zoneID, dnsID, domain, newIP string) error {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, dnsID)

	updateReq := DNSUpdateRequest{
		Type:    "A",
		Name:    domain,
		Content: newIP,
		TTL:     1,
		Proxied: false,
	}

	reqBytes, err := json.Marshal(updateReq)
	if err != nil {
		return fmt.Errorf("failed to marshal DNS update request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(reqBytes))
	if err != nil {
		return fmt.Errorf("failed creating PUT request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+authKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed executing DNS update PUT: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed reading DNS update response: %w", err)
	}

	var cfResp CloudflareResponse[DNSRecordResult]
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return fmt.Errorf("failed parsing DNS update response: %w (body: %s)", err, string(body))
	}

	if !cfResp.Success {
		return fmt.Errorf("Cloudflare DNS update failed: %s", formatCFErrors(cfResp.Errors))
	}

	return nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := NewLogger(cfg.LogPath)
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	logger.LogInfo(fmt.Sprintf("Starting Cloudflare Local DNS Updater for %s (check interval: %v, log: %s)",
		cfg.Domain, cfg.UpdateTime, cfg.LogPath))

	// Step 1: Check zone to get zone_identifier. Save in memory.
	zoneID, zoneName, err := findZoneIdentifier(httpClient, cfg.AuthKey, cfg.Domain)
	if err != nil {
		errMsg := fmt.Sprintf("Zone check failed: %v", err)
		logger.LogError(errMsg)
		os.Exit(1)
	}
	logger.LogSuccess(fmt.Sprintf("Zone check successful: %s", zoneName))

	// Step 2: Check DNS record to get dns_identifier and current stored IP. Save in memory.
	dnsRecord, err := findDNSRecord(httpClient, cfg.AuthKey, zoneID, cfg.Domain)
	if err != nil {
		errMsg := fmt.Sprintf("DNS record check failed: %v", err)
		logger.LogError(errMsg)
		os.Exit(1)
	}
	dnsID := dnsRecord.ID
	currentIP := dnsRecord.Content
	logger.LogSuccess(fmt.Sprintf("DNS check successful: %s, current IP=%s", cfg.Domain, currentIP))

	// Helper function to check and update IP if changed
	checkAndUpdateIP := func() {
		publicIP, err := fetchPublicIP(httpClient)
		if err != nil {
			logger.LogError(fmt.Sprintf("Public IP check failed: %v", err))
			return
		}

		// Only run an update to Cloudflare if any changes to the IP address
		if publicIP == currentIP {
			// IP unchanged; do not log success to keep logs clean
			return
		}

		logger.LogInfo(fmt.Sprintf("IP change detected: old=%s, new=%s. Updating Cloudflare DNS...", currentIP, publicIP))
		oldIP := currentIP
		if err := updateDNSRecord(httpClient, cfg.AuthKey, zoneID, dnsID, cfg.Domain, publicIP); err != nil {
			logger.LogError(fmt.Sprintf("Cloudflare DNS update error: %v", err))
			return
		}

		// Update in-memory IP
		currentIP = publicIP
		logger.LogSuccess(fmt.Sprintf("IP updated for %s: %s -> %s", cfg.Domain, oldIP, currentIP))
	}

	// Run initial IP check immediately
	checkAndUpdateIP()

	// Step 3: Run periodic update check every UPDATE_TIME
	ticker := time.NewTicker(cfg.UpdateTime)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.LogInfo(fmt.Sprintf("DNS updater running. Next check in %v...", cfg.UpdateTime))

	for {
		select {
		case <-ticker.C:
			checkAndUpdateIP()
		case sig := <-sigChan:
			logger.LogInfo(fmt.Sprintf("Received shutdown signal (%v). Exiting gracefully.", sig))
			return
		}
	}
}
