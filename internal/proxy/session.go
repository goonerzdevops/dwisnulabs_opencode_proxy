package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Session wire format required by the opencode.ai free tier:
//
//	ses_<12 hex><14 base62>
//
// The 12 hex characters are the low 48 bits of `~(timeMs*0x1000 + counter)`.
// Upstream decodes that prefix and rejects a non-small counter with
// 403 FreeTierError (live probes: counter 0-16 pass, >=100 fail), so the
// counter must stay a real per-millisecond sequence instead of a random number.
const (
	base62Chars      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sessionPrefixLen = 12
	sessionRandomLen = 14
	sessionPrefixMax = (uint64(1) << 48) - 1
	sessionTimeMax   = (uint64(1) << 36) - 1
)

// SessionPattern matches a well-formed canonical OpenCode session id.
var SessionPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)

var (
	clockMu          sync.Mutex
	lastTimestampMs  int64
	sessionSeqInTick uint64
)

func descendingTimeHex(ts uint64, counter uint64) string {
	return fmt.Sprintf("%0*x", sessionPrefixLen, ^(ts*0x1000+counter)&sessionPrefixMax)
}

func randomBase62(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is fatal for a proxy; a zero-filled fallback still
		// satisfies the wire regex instead of panicking on the hot path.
		return strings.Repeat("0", n)
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = base62Chars[int(b)%len(base62Chars)]
	}
	return string(out)
}

// GenerateSessionID mints a session id the way the OpenCode CLI does.
func GenerateSessionID(tsMillis int64) string {
	clockMu.Lock()
	if tsMillis != lastTimestampMs {
		lastTimestampMs = tsMillis
		sessionSeqInTick = 0
	}
	sessionSeqInTick++
	counter := sessionSeqInTick
	clockMu.Unlock()

	return "ses_" + descendingTimeHex(uint64(tsMillis), counter) + randomBase62(sessionRandomLen)
}

var (
	translatedMu       sync.Mutex
	translatedSessions = make(map[string]*sessionEntry)
)

// TranslateSessionID maps any downstream session identifier onto the canonical
// OpenCode format, minting from current timestamp instead of a sha256 digest
// (upstream rejects timestamps that fall outside current time window).
// Caches translated sessions per conversation so a session stays stable.
func TranslateSessionID(sessionID, clientTool string) string {
	trimmed := strings.TrimSpace(sessionID)
	if SessionPattern.MatchString(trimmed) {
		return trimmed
	}
	if clientTool == "" {
		clientTool = "generic"
	}

	key := clientTool + "\x00" + trimmed
	now := time.Now()

	translatedMu.Lock()
	defer translatedMu.Unlock()

	// Clean up oldest if capacity exceeded (max 5000)
	if len(translatedSessions) > 5000 {
		for k, v := range translatedSessions {
			if now.Sub(v.lastUsed) > 60*time.Minute {
				delete(translatedSessions, k)
			}
		}
	}

	if entry, ok := translatedSessions[key]; ok {
		entry.lastUsed = now
		return entry.id
	}

	// Mint with current time and pinned counter 1 (CLI behavior)
	translated := "ses_" + descendingTimeHex(uint64(now.UnixMilli()), 1) + randomBase62(sessionRandomLen)
	translatedSessions[key] = &sessionEntry{
		id:       translated,
		lastUsed: now,
	}
	return translated
}

type sessionEntry struct {
	id       string
	lastUsed time.Time
}

// SessionStore keeps one stable upstream session per downstream identity.
// Upstream bills free-tier quota per session, so spawning a fresh session per
// request triggers 429 FreeUsageLimitError.
type SessionStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	entries map[string]*sessionEntry
}

// NewSessionStore builds a TTL + LRU bounded store.
func NewSessionStore(ttl time.Duration, maxSize int) *SessionStore {
	if ttl <= 0 {
		ttl = 60 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &SessionStore{ttl: ttl, maxSize: maxSize, entries: make(map[string]*sessionEntry)}
}

// SetTTL updates the eviction horizon (used by config hot-reload).
func (s *SessionStore) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	s.mu.Lock()
	s.ttl = ttl
	s.mu.Unlock()
}

// Stable returns the canonical session id for identity, minting one on first use.
func (s *SessionStore) Stable(identity string) string {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictLocked(now)
	if entry, ok := s.entries[identity]; ok {
		entry.lastUsed = now
		return entry.id
	}
	if len(s.entries) >= s.maxSize {
		s.dropOldestLocked()
	}
	entry := &sessionEntry{id: GenerateSessionID(now.UnixMilli()), lastUsed: now}
	s.entries[identity] = entry
	return entry.id
}

// Len reports the number of live sessions (telemetry only).
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked(time.Now())
	return len(s.entries)
}

func (s *SessionStore) evictLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.Sub(entry.lastUsed) > s.ttl {
			delete(s.entries, key)
		}
	}
}

func (s *SessionStore) dropOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, entry := range s.entries {
		if oldestKey == "" || entry.lastUsed.Before(oldest) {
			oldestKey, oldest = key, entry.lastUsed
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

var (
	projectMu       sync.Mutex
	cachedProjectID string
)

// GetDefaultProjectID returns the project ID to present upstream.
// It checks local OpenCode SQLite storage if present, otherwise defaults
// to a persistent 40-character hex string as expected by the OpenCode v2 gate.
func GetDefaultProjectID() string {
	projectMu.Lock()
	defer projectMu.Unlock()
	if cachedProjectID != "" {
		return cachedProjectID
	}

	if local := findLocalOpenCodeProject(); local != "" {
		cachedProjectID = local
		return cachedProjectID
	}

	// Fallback to a synthetic 40-character hex string (20 random bytes).
	b := make([]byte, 20)
	if _, err := rand.Read(b); err == nil {
		cachedProjectID = hex.EncodeToString(b)
	} else {
		cachedProjectID = "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	}
	return cachedProjectID
}

func findLocalOpenCodeProject() string {
	var candidates []string
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		candidates = append(candidates, filepath.Join(localApp, "opencode", "opencode.db"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".local", "share", "opencode", "opencode.db"),
			filepath.Join(home, "Library", "Application Support", "opencode", "opencode.db"),
		)
	}

	for _, dbPath := range candidates {
		if _, err := os.Stat(dbPath); err == nil {
			for _, query := range []string{
				"SELECT project_id FROM session_v2 ORDER BY time_updated DESC LIMIT 1;",
				"SELECT project_id FROM session ORDER BY time_created DESC LIMIT 1;",
			} {
				out, err := exec.Command("sqlite3", dbPath, query).Output()
				if err == nil {
					p := strings.TrimSpace(string(out))
					if len(p) == 40 {
						return p
					}
				}
			}
		}
	}
	return ""
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", bytesLen*2)
	}
	return hex.EncodeToString(b)
}
