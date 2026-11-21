package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minRetryAfterDur = 1 * time.Second
	maxRetryAfterDur = 48 * time.Hour
	maxEgressLogSize = 10 << 20 // 10 MB
	maxRecentEvents  = 30
)

// ParseRetryAfter extracts an exact cooldown/reset duration from upstream HTTP headers
// (Retry-After or X-RateLimit-Reset), falling back to fallbackDur if absent or invalid.
func ParseRetryAfter(h http.Header, fallbackDur time.Duration) (time.Duration, string) {
	if fallbackDur <= 0 {
		fallbackDur = 12 * time.Hour
	}
	if h == nil {
		return fallbackDur, fmt.Sprintf("fallback:%s", fallbackDur.Round(time.Second))
	}

	if val := strings.TrimSpace(h.Get("Retry-After")); val != "" {
		if sec, err := strconv.ParseInt(val, 10, 64); err == nil && sec > 0 {
			dur := clampRetryDuration(time.Duration(sec) * time.Second)
			return dur, "header:Retry-After"
		}
		if t, err := http.ParseTime(val); err == nil {
			if diff := time.Until(t); diff > 0 {
				return clampRetryDuration(diff), "header:Retry-After"
			}
		}
	}

	for _, key := range []string{"X-RateLimit-Reset", "RateLimit-Reset"} {
		if val := strings.TrimSpace(h.Get(key)); val != "" {
			if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
				var dur time.Duration
				if n > 1_000_000_000 {
					dur = time.Until(time.Unix(n, 0))
				} else {
					dur = time.Duration(n) * time.Second
				}
				if dur > 0 {
					return clampRetryDuration(dur), "header:" + key
				}
			}
		}
	}

	return fallbackDur, fmt.Sprintf("fallback:%s", fallbackDur.Round(time.Second))
}

func clampRetryDuration(d time.Duration) time.Duration {
	if d < minRetryAfterDur {
		return minRetryAfterDur
	}
	if d > maxRetryAfterDur {
		return maxRetryAfterDur
	}
	return d
}

// ExtractIPv6Prefix64 parses an IP string and returns its canonical /64 prefix and whether it is IPv6.
func ExtractIPv6Prefix64(ipStr string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ipStr))
	if err != nil || !addr.Is6() {
		return "", false
	}
	p64, err := addr.Prefix(64)
	if err != nil {
		return "", false
	}
	return p64.Masked().String(), true
}

// extractIPv6Block32 returns the canonical /32 prefix (e.g. "2a09:bac1::/32") for an IPv6 address or prefix.
func extractIPv6Block32(ipOrPrefix string) string {
	raw := strings.TrimSpace(ipOrPrefix)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "/") {
		p, err := netip.ParsePrefix(raw)
		if err != nil || !p.Addr().Is6() {
			return ""
		}
		b32, err := p.Addr().Prefix(32)
		if err != nil {
			return ""
		}
		return b32.Masked().String()
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil || !addr.Is6() {
		return ""
	}
	b32, err := addr.Prefix(32)
	if err != nil {
		return ""
	}
	return b32.Masked().String()
}

// WarpRegResponse mirrors the Cloudflare WARP device registration response payload.
type WarpRegResponse struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	WarpEnabled bool   `json:"warp_enabled"`
	Config      struct {
		ClientID  string `json:"client_id"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		Peers []struct {
			PublicKey string `json:"public_key"`
			Endpoint  struct {
				V4   string `json:"v4"`
				V6   string `json:"v6"`
				Host string `json:"host"`
			} `json:"endpoint"`
		} `json:"peers"`
	} `json:"config"`
}

// WarpDevice stores the persistent WireGuard credentials for Cloudflare WARP.
type WarpDevice struct {
	PrivateKey string          `json:"private_key"`
	Reg        WarpRegResponse `json:"reg"`
	CreatedAt  time.Time       `json:"created_at"`
}

// WarpBurnEntry represents a single rate-limited or forbidden /64 prefix in warp-burned.json.
type WarpBurnEntry struct {
	IP           string    `json:"ip"`
	Prefix64     string    `json:"prefix_64"`
	BurnedAt     time.Time `json:"burned_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	ExpireSource string    `json:"expire_source"`
	HTTPStatus   int       `json:"http_status"`
	Reason       string    `json:"reason"`
	Colo         string    `json:"colo"`
}

// WarpBurnView is the JSON view returned by /api/warp/status with live countdown seconds.
type WarpBurnView struct {
	IP               string `json:"ip"`
	Prefix64         string `json:"prefix_64"`
	BurnedAt         string `json:"burned_at"`
	ExpiresAt        string `json:"expires_at"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	ExpireSource     string `json:"expire_source"`
	HTTPStatus       int    `json:"http_status"`
	Reason           string `json:"reason"`
	Colo             string `json:"colo"`
}

// WarpHealthyEntry tracks a WARP IPv6 /64 prefix that has served HTTP 200 responses.
type WarpHealthyEntry struct {
	IP             string
	Prefix64       string
	Colo           string
	Endpoint       string
	OKCount        int
	FirstOKAt      time.Time
	LastOKAt       time.Time
	LastLatencyMs  int64
	AvgLatencyMs   int64
	HandshakeMs    int64
	totalLatencyMs int64
}

// WarpHealthyView is the JSON view returned by /api/warp/status for HTTP 200 verified IPv6 prefixes.
type WarpHealthyView struct {
	IP            string `json:"ip"`
	Prefix64      string `json:"prefix_64"`
	Colo          string `json:"colo"`
	Endpoint      string `json:"endpoint"`
	OKCount       int    `json:"ok_count"`
	FirstOKAt     string `json:"first_ok_at"`
	LastOKAt      string `json:"last_ok_at"`
	LastLatencyMs int64  `json:"last_latency_ms"`
	AvgLatencyMs  int64  `json:"avg_latency_ms"`
	HandshakeMs   int64  `json:"handshake_ms"`
	Status        string `json:"status"` // "active", "standby", "healthy", "burned"
}

type warpBlockStat struct {
	ok     int
	burned int
}

// WarpStats aggregates historical counters from logs/warp_egress.jsonl and live requests.
type WarpStats struct {
	TotalOK200        int `json:"total_ok_200"`
	UniqueOKPrefixes  int `json:"unique_ok_prefixes"`
	TotalBurned429    int `json:"total_burned_429"`
	TotalForbidden403 int `json:"total_forbidden_403"`
	TotalHops         int `json:"total_hops"`
	StandbySwaps      int `json:"standby_swaps"`
}

// WarpLogEntry represents one line in logs/warp_egress.jsonl and recent transition events.
type WarpLogEntry struct {
	Timestamp     string `json:"ts"`
	Event         string `json:"event"`
	Endpoint      string `json:"endpoint,omitempty"`
	IP            string `json:"ip,omitempty"`
	Prefix64      string `json:"prefix_64,omitempty"`
	Colo          string `json:"colo,omitempty"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	RetryAfterSec int    `json:"retry_after_sec,omitempty"`
	ExpireSource  string `json:"expire_source,omitempty"`
	Reason        string `json:"reason,omitempty"`
	HandshakeMs   int64  `json:"handshake_ms,omitempty"`
	LatencyMs     int64  `json:"latency_ms,omitempty"`
}

// WarpStore manages warp-device.json, warp-burned.json, verified healthy IPv6 tracking,
// recent transition events, and logs/warp_egress.jsonl.
type WarpStore struct {
	mu           sync.Mutex
	dataDir      string
	devicePath   string
	burnedPath   string
	logPath      string
	device       *WarpDevice
	burned       map[string]WarpBurnEntry
	healthy      map[string]*WarpHealthyEntry
	blockStats   map[string]*warpBlockStat
	recentEvents []WarpLogEntry
	stats        WarpStats
	registerFn   func(ctx context.Context) (*WarpDevice, error)
}

// NewWarpStore initializes the store rooted at baseDir (typically filepath.Dir(configPath)).
func NewWarpStore(baseDir string) *WarpStore {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	dataDir := filepath.Join(baseDir, "data", "warp")
	logDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)

	s := &WarpStore{
		dataDir:      dataDir,
		devicePath:   filepath.Join(dataDir, "warp-device.json"),
		burnedPath:   filepath.Join(dataDir, "warp-burned.json"),
		logPath:      filepath.Join(logDir, "warp_egress.jsonl"),
		burned:       make(map[string]WarpBurnEntry),
		healthy:      make(map[string]*WarpHealthyEntry),
		blockStats:   make(map[string]*warpBlockStat),
		recentEvents: make([]WarpLogEntry, 0, maxRecentEvents),
	}
	s.registerFn = s.defaultRegisterDevice
	s.loadFromDisk()
	return s
}

// DataDir returns the directory where WARP runtime files and wireproxy live.
func (s *WarpStore) DataDir() string {
	return s.dataDir
}

// HasDevice reports whether a WARP device credential is already cached.
func (s *WarpStore) HasDevice() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.device != nil && s.device.PrivateKey != "" && len(s.device.Reg.Config.Peers) > 0
}

// EnsureDevice loads the persisted WARP device or registers a new one once.
func (s *WarpStore) EnsureDevice(ctx context.Context) (*WarpDevice, error) {
	s.mu.Lock()
	if s.device != nil && s.device.PrivateKey != "" && len(s.device.Reg.Config.Peers) > 0 {
		devCopy := *s.device
		s.mu.Unlock()
		return &devCopy, nil
	}
	regFn := s.registerFn
	s.mu.Unlock()

	dev, err := regFn(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.device = dev
	_ = atomicWriteJSON(s.devicePath, dev, 0o600)
	s.mu.Unlock()
	return dev, nil
}

// IsBurned checks in real-time whether prefixOrIP is currently burned.
// If the entry has expired, it immediately returns false.
func (s *WarpStore) IsBurned(prefixOrIP string) (WarpBurnEntry, bool) {
	key := strings.TrimSpace(prefixOrIP)
	if key == "" {
		return WarpBurnEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.burned[key]
	if !ok {
		return WarpBurnEntry{}, false
	}
	if !time.Now().Before(entry.ExpiresAt) {
		delete(s.burned, key)
		return WarpBurnEntry{}, false
	}
	return entry, true
}

// IsBlockCongested reports whether the /32 IPv6 block (e.g. 2a09:bac1::/32)
// has consistently failed with zero HTTP 200 successes:
//   - >= 6 burns with 0 OKs once at least one other /32 block has proven healthy (ok > 0), OR
//   - >= 12 burns with 0 OKs unconditionally.
// This prevents a fresh deployment from prematurely blacklisting its primary /32 block
// after just a couple of busy shared prefixes.
func (s *WarpStore) IsBlockCongested(prefixOrIP string) bool {
	b32 := extractIPv6Block32(prefixOrIP)
	if b32 == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.blockStats[b32]
	if !ok || st.ok > 0 {
		return false
	}
	if st.burned >= 12 {
		return true
	}
	if st.burned >= 6 {
		for other, ost := range s.blockStats {
			if other != b32 && ost.ok > 0 {
				return true
			}
		}
	}
	return false
}

// ActiveBurnedCount returns the number of currently active (non-expired) burned prefixes.
func (s *WarpStore) ActiveBurnedCount() int {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, e := range s.burned {
		if !now.Before(e.ExpiresAt) {
			delete(s.burned, k)
			continue
		}
		count++
	}
	return count
}

// MarkBurned records a rejected IPv6 /64 prefix (or IP fallback), prunes expired entries, and persists to disk.
func (s *WarpStore) MarkBurned(ip, prefix64, colo string, status int, reason string, dur time.Duration, src string) WarpBurnEntry {
	now := time.Now()
	if dur <= 0 {
		dur = 12 * time.Hour
	}
	key := strings.TrimSpace(prefix64)
	if key == "" {
		key = strings.TrimSpace(ip)
	}
	entry := WarpBurnEntry{
		IP:           strings.TrimSpace(ip),
		Prefix64:     strings.TrimSpace(prefix64),
		BurnedAt:     now,
		ExpiresAt:    now.Add(dur),
		ExpireSource: src,
		HTTPStatus:   status,
		Reason:       reason,
		Colo:         colo,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if key != "" {
		s.burned[key] = entry
		_ = atomicWriteJSON(s.burnedPath, s.burned, 0o644)
	}
	return entry
}

// ActiveBurned prunes expired entries and returns the active burned list sorted by earliest expiry.
func (s *WarpStore) ActiveBurned() []WarpBurnView {
	now := time.Now()
	s.mu.Lock()
	pruned := s.pruneLocked(now)
	if pruned {
		_ = atomicWriteJSON(s.burnedPath, s.burned, 0o644)
	}
	out := make([]WarpBurnView, 0, len(s.burned))
	for _, e := range s.burned {
		rem := int(e.ExpiresAt.Sub(now).Seconds())
		if rem < 0 {
			rem = 0
		}
		out = append(out, WarpBurnView{
			IP:               e.IP,
			Prefix64:         e.Prefix64,
			BurnedAt:         e.BurnedAt.Format(time.RFC3339),
			ExpiresAt:        e.ExpiresAt.Format(time.RFC3339),
			ExpiresInSeconds: rem,
			ExpireSource:     e.ExpireSource,
			HTTPStatus:       e.HTTPStatus,
			Reason:           e.Reason,
			Colo:             e.Colo,
		})
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].ExpiresInSeconds < out[j].ExpiresInSeconds
	})
	return out
}

// ClearBurned wipes all active burned entries and persists the empty map.
func (s *WarpStore) ClearBurned() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.burned = make(map[string]WarpBurnEntry)
	_ = atomicWriteJSON(s.burnedPath, s.burned, 0o644)
}

// RecordOK records every successful HTTP 200 response served over a WARP IPv6 tunnel,
// updating accurate per-request counters, per-prefix healthy history, and JSONL logs.
func (s *WarpStore) RecordOK(ip, prefix64, colo, endpoint string, handshakeMs, latencyMs int64, firstForGen bool) {
	event := "ok_200"
	if firstForGen {
		event = "hop_active"
	}
	s.AppendLog(WarpLogEntry{
		Timestamp:   time.Now().Format(time.RFC3339),
		Event:       event,
		Endpoint:    endpoint,
		IP:          ip,
		Prefix64:    prefix64,
		Colo:        colo,
		HTTPStatus:  http.StatusOK,
		HandshakeMs: handshakeMs,
		LatencyMs:   latencyMs,
	})
}

// AppendLog writes an audit event to logs/warp_egress.jsonl and updates counters,
// verified healthy IPv6 tracking, block statistics, and the recent transition timeline.
func (s *WarpStore) AppendLog(entry WarpLogEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.applyLogEntryLocked(entry, time.Now())
	s.writeLogLineLocked(entry)
}

// RecordEvent adds a transition/diagnostic event to the recent timeline without
// counting it as an upstream HTTP status failure (and persists to JSONL).
func (s *WarpStore) RecordEvent(entry WarpLogEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.Event == "swap_standby" {
		s.stats.StandbySwaps++
		s.stats.TotalHops++
	}
	s.pushRecentEventLocked(entry)
	s.writeLogLineLocked(entry)
}

// HealthyList returns all IPv6 /64 prefixes that have served HTTP 200 responses,
// annotated with their current live state ("active", "standby", "healthy", or "burned").
func (s *WarpStore) HealthyList(activePrefix, standbyPrefix string) []WarpHealthyView {
	now := time.Now()
	s.mu.Lock()
	s.pruneLocked(now)

	out := make([]WarpHealthyView, 0, len(s.healthy))
	for _, h := range s.healthy {
		st := "healthy"
		if activePrefix != "" && h.Prefix64 == activePrefix {
			st = "active"
		} else if standbyPrefix != "" && h.Prefix64 == standbyPrefix {
			st = "standby"
		} else if _, burned := s.burned[h.Prefix64]; burned {
			st = "burned"
		}
		out = append(out, WarpHealthyView{
			IP:            h.IP,
			Prefix64:      h.Prefix64,
			Colo:          h.Colo,
			Endpoint:      h.Endpoint,
			OKCount:       h.OKCount,
			FirstOKAt:     h.FirstOKAt.Format(time.RFC3339),
			LastOKAt:      h.LastOKAt.Format(time.RFC3339),
			LastLatencyMs: h.LastLatencyMs,
			AvgLatencyMs:  h.AvgLatencyMs,
			HandshakeMs:   h.HandshakeMs,
			Status:        st,
		})
	}
	s.mu.Unlock()

	rank := func(st string) int {
		switch st {
		case "active":
			return 0
		case "standby":
			return 1
		case "healthy":
			return 2
		default:
			return 3
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank(out[i].Status), rank(out[j].Status)
		if ri != rj {
			return ri < rj
		}
		if out[i].LastOKAt != out[j].LastOKAt {
			return out[i].LastOKAt > out[j].LastOKAt
		}
		return out[i].OKCount > out[j].OKCount
	})
	return out
}

// RecentEvents returns up to limit recent transition/egress events, newest first.
func (s *WarpStore) RecentEvents(limit int) []WarpLogEntry {
	if limit <= 0 || limit > maxRecentEvents {
		limit = maxRecentEvents
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.recentEvents)
	if n == 0 {
		return []WarpLogEntry{}
	}
	if limit > n {
		limit = n
	}
	out := make([]WarpLogEntry, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		out = append(out, s.recentEvents[i])
	}
	return out
}

// Stats returns a snapshot of the WARP egress audit counters.
func (s *WarpStore) Stats() WarpStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.UniqueOKPrefixes = len(s.healthy)
	return st
}

func (s *WarpStore) applyLogEntryLocked(entry WarpLogEntry, fallbackTime time.Time) {
	ts := fallbackTime
	if parsed, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
		ts = parsed
	}
	key := strings.TrimSpace(entry.Prefix64)
	if key == "" {
		key = strings.TrimSpace(entry.IP)
	}

	switch entry.HTTPStatus {
	case http.StatusOK:
		s.stats.TotalOK200++
		if key != "" {
			h, exists := s.healthy[key]
			if !exists {
				h = &WarpHealthyEntry{
					Prefix64:  key,
					FirstOKAt: ts,
				}
				s.healthy[key] = h
			}
			if entry.IP != "" {
				h.IP = entry.IP
			}
			if entry.Colo != "" {
				h.Colo = entry.Colo
			}
			if entry.Endpoint != "" {
				h.Endpoint = entry.Endpoint
			}
			if entry.HandshakeMs > 0 {
				h.HandshakeMs = entry.HandshakeMs
			}
			h.OKCount++
			if h.FirstOKAt.IsZero() || ts.Before(h.FirstOKAt) {
				h.FirstOKAt = ts
			}
			if ts.After(h.LastOKAt) || h.LastOKAt.IsZero() {
				h.LastOKAt = ts
				if entry.LatencyMs > 0 {
					h.LastLatencyMs = entry.LatencyMs
				}
			}
			if entry.LatencyMs > 0 {
				h.totalLatencyMs += entry.LatencyMs
				h.AvgLatencyMs = h.totalLatencyMs / int64(h.OKCount)
			}
			s.recordBlockStatLocked(key, true)
		}
		s.stats.UniqueOKPrefixes = len(s.healthy)
	case http.StatusTooManyRequests:
		s.stats.TotalBurned429++
		if key != "" {
			s.recordBlockStatLocked(key, false)
		}
	case http.StatusForbidden:
		s.stats.TotalForbidden403++
		if key != "" {
			s.recordBlockStatLocked(key, false)
		}
	}

	if entry.Event == "swap_standby" {
		s.stats.StandbySwaps++
		s.stats.TotalHops++
	} else if strings.HasPrefix(entry.Event, "hop_") {
		s.stats.TotalHops++
	}

	s.pushRecentEventLocked(entry)
}

func (s *WarpStore) recordBlockStatLocked(prefixOrIP string, ok bool) {
	b32 := extractIPv6Block32(prefixOrIP)
	if b32 == "" {
		return
	}
	st, exists := s.blockStats[b32]
	if !exists {
		st = &warpBlockStat{}
		s.blockStats[b32] = st
	}
	if ok {
		st.ok++
	} else {
		st.burned++
	}
}

func (s *WarpStore) pushRecentEventLocked(entry WarpLogEntry) {
	if len(s.recentEvents) >= maxRecentEvents {
		copy(s.recentEvents, s.recentEvents[1:])
		s.recentEvents[maxRecentEvents-1] = entry
		return
	}
	s.recentEvents = append(s.recentEvents, entry)
}

func (s *WarpStore) writeLogLineLocked(entry WarpLogEntry) {
	if info, err := os.Stat(s.logPath); err == nil && info.Size() >= maxEgressLogSize {
		_ = os.Rename(s.logPath, s.logPath+".1")
	}

	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func (s *WarpStore) pruneLocked(now time.Time) bool {
	changed := false
	for k, e := range s.burned {
		if !now.Before(e.ExpiresAt) {
			delete(s.burned, k)
			changed = true
		}
	}
	return changed
}

func (s *WarpStore) loadFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if raw, err := os.ReadFile(s.devicePath); err == nil {
		var dev WarpDevice
		if err := json.Unmarshal(raw, &dev); err == nil && dev.PrivateKey != "" && len(dev.Reg.Config.Peers) > 0 {
			s.device = &dev
		}
	}

	if raw, err := os.ReadFile(s.burnedPath); err == nil {
		var m map[string]WarpBurnEntry
		if err := json.Unmarshal(raw, &m); err == nil && m != nil {
			s.burned = m
			if s.pruneLocked(time.Now()) {
				_ = atomicWriteJSON(s.burnedPath, s.burned, 0o644)
			}
		}
	}

	if f, err := os.Open(s.logPath); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		now := time.Now()
		for sc.Scan() {
			var e WarpLogEntry
			if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
				s.applyLogEntryLocked(e, now)
			}
		}
	}
}

func (s *WarpStore) defaultRegisterDevice(ctx context.Context) (*WarpDevice, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate x25519 key: %w", err)
	}
	privB64 := base64.StdEncoding.EncodeToString(priv.Bytes())
	pubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())

	payload := map[string]any{
		"key":           pubB64,
		"install_id":    "",
		"fcm_token":     "",
		"tos":           time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"model":         "PC",
		"serial_number": "",
		"locale":        "en_US",
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.cloudflareclient.com/v0a2158/reg", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("CF-Client-Version", "a-6.11-2223")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register warp device: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("warp reg HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var reg WarpRegResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("decode warp reg: %w", err)
	}
	if len(reg.Config.Peers) == 0 {
		return nil, fmt.Errorf("warp reg returned no peers")
	}

	// Empirically verified in PoC: 2-second propagation wait prevents first-handshake timeouts.
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return &WarpDevice{
		PrivateKey: privB64,
		Reg:        reg,
		CreatedAt:  time.Now(),
	}, nil
}

func atomicWriteJSON(path string, v any, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')

	tmp, err := os.CreateTemp(dir, ".warp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(blob); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(perm)
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
