package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const maxBodyBytes = 64 << 10

var (
	errInvalidIP   = errors.New("invalid_ip")
	errNonPublicIP = errors.New("non_public_ip")
)

type config struct {
	listen           string
	cityDBPath       string
	asnDBPath        string
	secret           []byte
	allowedProjects  map[string]struct{}
	maxBatch         int
	maxPerMinute     int
	cacheTTL         time.Duration
	cacheSize        int
	authMaxClockSkew time.Duration
}

type server struct {
	config    config
	databases databaseStore
	cache     lookupCache
	limits    projectLimiter
	nonces    nonceStore
}

type databaseStore struct {
	mu        sync.RWMutex
	city      *maxminddb.Reader
	asn       *maxminddb.Reader
	cityPath  string
	asnPath   string
	cityMTime time.Time
	asnMTime  time.Time
}

type lookupCache struct {
	mu      sync.Mutex
	items   map[string]cacheItem
	ttl     time.Duration
	maxSize int
}

type cacheItem struct {
	value   lookupResponse
	expires time.Time
}

type projectLimiter struct {
	mu      sync.Mutex
	max     int
	current map[string]minuteBucket
}

type minuteBucket struct {
	minute int64
	count  int
}

type nonceStore struct {
	mu    sync.Mutex
	items map[string]time.Time
	ttl   time.Duration
}

type lookupRequest struct {
	IP string `json:"ip"`
}

type batchRequest struct {
	IPs []string `json:"ips"`
}

type lookupResponse struct {
	IP             string  `json:"ip"`
	CountryCode    string  `json:"country_code,omitempty"`
	CountryName    string  `json:"country_name,omitempty"`
	RegionCode     string  `json:"region_code,omitempty"`
	RegionName     string  `json:"region_name,omitempty"`
	City           string  `json:"city,omitempty"`
	PostalCode     string  `json:"postal_code,omitempty"`
	Timezone       string  `json:"timezone,omitempty"`
	Latitude       float64 `json:"latitude,omitempty"`
	Longitude      float64 `json:"longitude,omitempty"`
	AccuracyRadius uint16  `json:"accuracy_radius_km,omitempty"`
	ASN            uint    `json:"asn,omitempty"`
	ASName         string  `json:"as_name,omitempty"`
	DatabaseAt     string  `json:"database_updated_at,omitempty"`
}

type healthResponse struct {
	Status       string `json:"status"`
	CityDatabase string `json:"city_database"`
	ASNDatabase  string `json:"asn_database"`
}

type cityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Postal struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"postal"`
	Location struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		TimeZone       string  `maxminddb:"time_zone"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
}

type asnRecord struct {
	Number uint   `maxminddb:"autonomous_system_number"`
	Name   string `maxminddb:"autonomous_system_organization"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	s := &server{
		config: cfg,
		cache: lookupCache{
			items:   make(map[string]cacheItem),
			ttl:     cfg.cacheTTL,
			maxSize: cfg.cacheSize,
		},
		limits: projectLimiter{max: cfg.maxPerMinute, current: make(map[string]minuteBucket)},
		nonces: nonceStore{items: make(map[string]time.Time), ttl: cfg.authMaxClockSkew},
	}
	if err := s.databases.reload(cfg.cityDBPath, cfg.asnDBPath); err != nil {
		log.Fatalf("open GeoIP databases: %v", err)
	}

	go s.reloadLoop()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/lookup", s.handleLookup)
	mux.HandleFunc("POST /v1/lookup/batch", s.handleBatch)

	httpServer := &http.Server{
		Addr:              cfg.listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       8 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("jcm-geoip listening on %s", cfg.listen)
	log.Fatal(httpServer.ListenAndServe())
}

func loadConfig() (config, error) {
	secret := strings.TrimSpace(os.Getenv("JCM_GEOIP_HMAC_SECRET"))
	if secret == "" {
		return config{}, errors.New("JCM_GEOIP_HMAC_SECRET is required")
	}
	maxBatch, err := envInt("JCM_GEOIP_MAX_BATCH", 100, 1, 500)
	if err != nil {
		return config{}, err
	}
	maxPerMinute, err := envInt("JCM_GEOIP_MAX_REQUESTS_PER_MINUTE", 600, 1, 50000)
	if err != nil {
		return config{}, err
	}
	cacheHours, err := envInt("JCM_GEOIP_CACHE_TTL_HOURS", 168, 1, 24*90)
	if err != nil {
		return config{}, err
	}
	cacheSize, err := envInt("JCM_GEOIP_CACHE_SIZE", 20000, 100, 500000)
	if err != nil {
		return config{}, err
	}
	projects := make(map[string]struct{})
	for _, project := range strings.Split(os.Getenv("JCM_GEOIP_ALLOWED_PROJECTS"), ",") {
		if value := strings.TrimSpace(project); value != "" {
			projects[value] = struct{}{}
		}
	}
	if len(projects) == 0 {
		return config{}, errors.New("JCM_GEOIP_ALLOWED_PROJECTS must contain at least one stable project id")
	}
	return config{
		listen:           envString("JCM_GEOIP_LISTEN", ":8080"),
		cityDBPath:       envString("JCM_GEOIP_CITY_DB", "/var/lib/jcm-geoip/GeoLite2-City.mmdb"),
		asnDBPath:        envString("JCM_GEOIP_ASN_DB", "/var/lib/jcm-geoip/GeoLite2-ASN.mmdb"),
		secret:           []byte(secret),
		allowedProjects:  projects,
		maxBatch:         maxBatch,
		maxPerMinute:     maxPerMinute,
		cacheTTL:         time.Duration(cacheHours) * time.Hour,
		cacheSize:        cacheSize,
		authMaxClockSkew: 5 * time.Minute,
	}, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback, min, max int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return parsed, nil
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.databases.mu.RLock()
	response := healthResponse{
		Status:       "ok",
		CityDatabase: s.databases.cityMTime.UTC().Format(time.RFC3339),
		ASNDatabase:  s.databases.asnMTime.UTC().Format(time.RFC3339),
	}
	s.databases.mu.RUnlock()
	writeJSON(w, http.StatusOK, response)
}

func (s *server) handleLookup(w http.ResponseWriter, r *http.Request) {
	body, project, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var input lookupRequest
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	result, err := s.lookup(input.IP)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	w.Header().Set("X-JCM-Project", project)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handleBatch(w http.ResponseWriter, r *http.Request) {
	body, project, ok := s.authorize(w, r)
	if !ok {
		return
	}
	var input batchRequest
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if len(input.IPs) == 0 || len(input.IPs) > s.config.maxBatch {
		writeError(w, http.StatusBadRequest, "invalid_batch_size")
		return
	}
	results := make([]lookupResponse, 0, len(input.IPs))
	for _, rawIP := range input.IPs {
		result, err := s.lookup(rawIP)
		if err != nil {
			continue
		}
		results = append(results, result)
	}
	w.Header().Set("X-JCM-Project", project)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *server) authorize(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "body_required")
		return nil, "", false
	}
	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	content, err := io.ReadAll(body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large")
		return nil, "", false
	}
	project := strings.TrimSpace(r.Header.Get("X-JCM-Project"))
	timestamp := strings.TrimSpace(r.Header.Get("X-JCM-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("X-JCM-Nonce"))
	signature := strings.TrimSpace(r.Header.Get("X-JCM-Signature"))
	if project == "" || timestamp == "" || nonce == "" || signature == "" || len(nonce) > 128 {
		writeError(w, http.StatusUnauthorized, "signature_required")
		return nil, "", false
	}
	if _, allowed := s.config.allowedProjects[project]; !allowed {
		writeError(w, http.StatusForbidden, "project_not_allowed")
		return nil, "", false
	}
	parsedTimestamp, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || time.Since(parsedTimestamp) > s.config.authMaxClockSkew || parsedTimestamp.Sub(time.Now()) > s.config.authMaxClockSkew {
		writeError(w, http.StatusUnauthorized, "expired_signature")
		return nil, "", false
	}
	bodyHash := sha256.Sum256(content)
	canonical := project + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, s.config.secret)
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(signature)), []byte(expected)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_signature")
		return nil, "", false
	}
	if !s.nonces.claim(project + ":" + nonce) {
		writeError(w, http.StatusConflict, "replayed_nonce")
		return nil, "", false
	}
	if !s.limits.allow(project) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return nil, "", false
	}
	return content, project, true
}

func (s *server) lookup(rawIP string) (lookupResponse, error) {
	addr, err := publicAddress(rawIP)
	if err != nil {
		return lookupResponse{}, err
	}
	key := addr.String()
	if value, ok := s.cache.get(key); ok {
		return value, nil
	}

	result := lookupResponse{IP: key}
	s.databases.mu.RLock()
	var city cityRecord
	if err := s.databases.city.Lookup(net.IP(addr.AsSlice()), &city); err != nil {
		s.databases.mu.RUnlock()
		return lookupResponse{}, fmt.Errorf("city lookup: %w", err)
	}
	var asn asnRecord
	if err := s.databases.asn.Lookup(net.IP(addr.AsSlice()), &asn); err != nil {
		s.databases.mu.RUnlock()
		return lookupResponse{}, fmt.Errorf("asn lookup: %w", err)
	}
	updated := s.databases.cityMTime.UTC().Format(time.RFC3339)
	s.databases.mu.RUnlock()

	result.CountryCode = city.Country.ISOCode
	result.CountryName = city.Country.Names["en"]
	result.City = city.City.Names["en"]
	result.PostalCode = city.Postal.Code
	result.Timezone = city.Location.TimeZone
	result.Latitude = city.Location.Latitude
	result.Longitude = city.Location.Longitude
	result.AccuracyRadius = city.Location.AccuracyRadius
	result.ASN = asn.Number
	result.ASName = asn.Name
	result.DatabaseAt = updated
	if len(city.Subdivisions) > 0 {
		result.RegionCode = city.Subdivisions[0].ISOCode
		result.RegionName = city.Subdivisions[0].Names["en"]
	}
	s.cache.set(key, result)
	return result, nil
}

func publicAddress(value string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}, errInvalidIP
	}
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return netip.Addr{}, errNonPublicIP
	}
	return addr, nil
}

func (d *databaseStore) reload(cityPath, asnPath string) error {
	cityInfo, err := os.Stat(cityPath)
	if err != nil {
		return fmt.Errorf("city database: %w", err)
	}
	asnInfo, err := os.Stat(asnPath)
	if err != nil {
		return fmt.Errorf("asn database: %w", err)
	}
	newCity, err := maxminddb.Open(cityPath)
	if err != nil {
		return err
	}
	newASN, err := maxminddb.Open(asnPath)
	if err != nil {
		_ = newCity.Close()
		return err
	}
	d.mu.Lock()
	oldCity, oldASN := d.city, d.asn
	d.city, d.asn = newCity, newASN
	d.cityPath, d.asnPath = cityPath, asnPath
	d.cityMTime, d.asnMTime = cityInfo.ModTime(), asnInfo.ModTime()
	d.mu.Unlock()
	if oldCity != nil {
		_ = oldCity.Close()
	}
	if oldASN != nil {
		_ = oldASN.Close()
	}
	return nil
}

func (s *server) reloadLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if s.databases.changed() {
			if err := s.databases.reload(s.config.cityDBPath, s.config.asnDBPath); err != nil {
				log.Printf("GeoIP reload failed: %v", err)
				continue
			}
			s.cache.clear()
			log.Print("GeoIP databases reloaded")
		}
	}
}

func (d *databaseStore) changed() bool {
	cityInfo, cityErr := os.Stat(d.cityPath)
	asnInfo, asnErr := os.Stat(d.asnPath)
	if cityErr != nil || asnErr != nil {
		return false
	}
	d.mu.RLock()
	changed := cityInfo.ModTime().After(d.cityMTime) || asnInfo.ModTime().After(d.asnMTime)
	d.mu.RUnlock()
	return changed
}

func (c *lookupCache) get(key string) (lookupResponse, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok || !item.expires.After(now) {
		if ok {
			delete(c.items, key)
		}
		return lookupResponse{}, false
	}
	return item.value, true
}

func (c *lookupCache) set(key string, value lookupResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.maxSize {
		keys := make([]string, 0, len(c.items))
		for itemKey := range c.items {
			keys = append(keys, itemKey)
		}
		sort.Strings(keys)
		for _, itemKey := range keys[:max(1, len(keys)/10)] {
			delete(c.items, itemKey)
		}
	}
	c.items[key] = cacheItem{value: value, expires: time.Now().Add(c.ttl)}
}

func (c *lookupCache) clear() {
	c.mu.Lock()
	c.items = make(map[string]cacheItem)
	c.mu.Unlock()
}

func (l *projectLimiter) allow(project string) bool {
	nowMinute := time.Now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.current[project]
	if bucket.minute != nowMinute {
		bucket = minuteBucket{minute: nowMinute}
	}
	if bucket.count >= l.max {
		return false
	}
	bucket.count++
	l.current[project] = bucket
	return true
}

func (n *nonceStore) claim(key string) bool {
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	for storedKey, expires := range n.items {
		if !expires.After(now) {
			delete(n.items, storedKey)
		}
	}
	if _, exists := n.items[key]; exists {
		return false
	}
	n.items[key] = now.Add(n.ttl)
	return true
}

func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidIP) || errors.Is(err, errNonPublicIP) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusServiceUnavailable, "lookup_unavailable")
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
