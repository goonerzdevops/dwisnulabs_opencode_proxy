// Package config loads, validates and atomically persists the proxy settings.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Environment overrides.
const (
	EnvConfigPath = "OPENCODE_PROXY_CONFIG" // full path to config.json
	EnvPort       = "OPENCODE_PROXY_PORT"   // overrides Config.Port
	EnvHost       = "OPENCODE_PROXY_HOST"   // overrides Config.Host
)

// Config is the persisted proxy configuration (config.json).
type Config struct {
	Port                   int               `json:"port"`
	Host                   string            `json:"host"`
	APIKeys                []string          `json:"api_keys"`
	PermissiveAuth         bool              `json:"permissive_auth"`
	DefaultModel           string            `json:"default_model"`
	ModelAliases           map[string]string `json:"model_aliases"`
	UpstreamBaseURL        string            `json:"upstream_base_url"`
	SessionTTLMinutes      int               `json:"session_ttl_minutes"`
	UpstreamTimeoutSeconds int               `json:"upstream_timeout_seconds"`
	LogRequests            bool              `json:"log_requests"`
	FollowUpstreamModels   bool              `json:"follow_upstream_models"`
	SystemPromptTuning     string            `json:"system_prompt_tuning"`
	Proxies                []string          `json:"proxies"`
	ProxyCooldownSeconds   int               `json:"proxy_cooldown_seconds"`
	AntiAbuseEnabled       bool              `json:"anti_abuse_enabled"`
	MaxConcurrentPerNode   int               `json:"max_concurrent_per_node"`
	MinRequestIntervalMs   int               `json:"min_request_interval_ms"`
	PuterAPIKey            string            `json:"puter_api_key"`
	PuterEnabled           bool              `json:"puter_enabled"`
	PuterDefaultModel      string            `json:"puter_default_model"`
	PuterAccounts          []PuterAccount    `json:"puter_accounts"`
	KiloEnabled            bool              `json:"kilo_enabled"`
	KiloDefaultModel       string            `json:"kilo_default_model"`
}

// PuterAccount represents a Puter.com account in the multi-account pool.
type PuterAccount struct {
	ID            string     `json:"id"`
	Username      string     `json:"username"`
	Token         string     `json:"token"`
	Status        string     `json:"status"` // "active", "cooldown", "rate_limited", "disabled"
	Priority      int        `json:"priority"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// Default returns the out-of-the-box configuration described in plan.md.
func Default() Config {
	return Config{
		Port:           20144,
		Host:           "127.0.0.1",
		APIKeys:        []string{"sk-opencode-free"},
		PermissiveAuth: true,
		DefaultModel:   "big-pickle",
		ModelAliases:   map[string]string{},
		UpstreamBaseURL:        "https://opencode.ai",
		SessionTTLMinutes:      60,
		UpstreamTimeoutSeconds: 600,
		LogRequests:            true,
		FollowUpstreamModels:   true,
		SystemPromptTuning:     "Berikan jawaban dengan artikulasi yang jelas, lengkap, dan tuntas dalam bahasa apa pun yang digunakan oleh pengguna, tanpa kata atau kalimat yang terpotong dan tanpa kebocoran istilah bahasa asing yang tidak relevan. Saat menulis kode dalam bahasa pemrograman apa pun, tuliskan kode secara utuh, bersih, valid secara sintaksis, dan siap pakai tanpa pemotongan atau placeholder yang tidak lengkap.",
		Proxies:                []string{"direct"},
		ProxyCooldownSeconds:   900,
		AntiAbuseEnabled:       true,
		MaxConcurrentPerNode:   1,
		MinRequestIntervalMs:   300,
		PuterAPIKey:            "",
		PuterEnabled:           false,
		PuterDefaultModel:      "claude-opus-5-5",
		PuterAccounts:          []PuterAccount{},
		KiloEnabled:            true,
		KiloDefaultModel:       "openrouter/free",
	}
}

// Clone returns a deep copy so callers can never mutate live state.
func (c Config) Clone() Config {
	out := c
	out.APIKeys = append([]string(nil), c.APIKeys...)
	out.Proxies = append([]string(nil), c.Proxies...)
	out.ModelAliases = make(map[string]string, len(c.ModelAliases))
	for k, v := range c.ModelAliases {
		out.ModelAliases[k] = v
	}
	if len(c.PuterAccounts) > 0 {
		out.PuterAccounts = make([]PuterAccount, len(c.PuterAccounts))
		copy(out.PuterAccounts, c.PuterAccounts)
	}
	return out
}

// Normalize fills empty fields with defaults and canonicalises map keys.
func (c *Config) Normalize() {
	def := Default()
	if c.Port == 0 {
		c.Port = def.Port
	}
	if strings.TrimSpace(c.Host) == "" {
		c.Host = def.Host
	}
	if c.DefaultModel == "" {
		c.DefaultModel = def.DefaultModel
	}
	if c.UpstreamBaseURL == "" {
		c.UpstreamBaseURL = def.UpstreamBaseURL
	}
	c.UpstreamBaseURL = strings.TrimRight(c.UpstreamBaseURL, "/")
	if c.SessionTTLMinutes <= 0 {
		c.SessionTTLMinutes = def.SessionTTLMinutes
	}
	if c.UpstreamTimeoutSeconds <= 0 {
		c.UpstreamTimeoutSeconds = def.UpstreamTimeoutSeconds
	}
	if c.ModelAliases == nil {
		c.ModelAliases = def.ModelAliases
	}
	aliases := make(map[string]string, len(c.ModelAliases))
	for k, v := range c.ModelAliases {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" || strings.TrimSpace(v) == "" {
			continue
		}
		aliases[key] = strings.TrimSpace(v)
	}
	c.ModelAliases = aliases
	keys := make([]string, 0, len(c.APIKeys))
	for _, k := range c.APIKeys {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	c.APIKeys = keys
	if c.SystemPromptTuning == "" {
		c.SystemPromptTuning = def.SystemPromptTuning
	}
	if len(c.Proxies) == 0 {
		c.Proxies = def.Proxies
	}
	if c.ProxyCooldownSeconds <= 0 {
		c.ProxyCooldownSeconds = def.ProxyCooldownSeconds
	}
	if c.MaxConcurrentPerNode <= 0 {
		c.MaxConcurrentPerNode = def.MaxConcurrentPerNode
	}
	if c.MinRequestIntervalMs <= 0 {
		c.MinRequestIntervalMs = def.MinRequestIntervalMs
	}
	if strings.TrimSpace(c.PuterDefaultModel) == "" {
		c.PuterDefaultModel = def.PuterDefaultModel
	}
	if strings.TrimSpace(c.KiloDefaultModel) == "" {
		c.KiloDefaultModel = def.KiloDefaultModel
	}
	c.PuterAPIKey = strings.TrimSpace(c.PuterAPIKey)

	// Auto-migration: if PuterAPIKey exists but PuterAccounts is empty, populate PuterAccounts
	if len(c.PuterAccounts) == 0 && c.PuterAPIKey != "" {
		c.PuterAccounts = append(c.PuterAccounts, PuterAccount{
			ID:       "acc-default",
			Username: "default",
			Token:    c.PuterAPIKey,
			Status:   "active",
			Priority: 1,
		})
	}

	// Sanitize accounts
	validAccs := make([]PuterAccount, 0, len(c.PuterAccounts))
	for i, acc := range c.PuterAccounts {
		acc.ID = strings.TrimSpace(acc.ID)
		if acc.ID == "" {
			acc.ID = fmt.Sprintf("acc-%d", i+1)
		}
		acc.Username = strings.TrimSpace(acc.Username)
		if acc.Username == "" {
			acc.Username = fmt.Sprintf("account_%d", i+1)
		}
		acc.Token = strings.TrimSpace(acc.Token)
		if acc.Token == "" {
			continue
		}
		acc.Status = strings.ToLower(strings.TrimSpace(acc.Status))
		if acc.Status == "" {
			acc.Status = "active"
		}
		if acc.Priority <= 0 {
			acc.Priority = i + 1
		}
		validAccs = append(validAccs, acc)
	}
	c.PuterAccounts = validAccs
	if len(c.PuterAccounts) > 0 && c.PuterAPIKey == "" {
		c.PuterAPIKey = c.PuterAccounts[0].Token
	}
}

// Validate rejects configurations that would make the daemon unusable.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range 1-65535", c.Port)
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("host must not be empty")
	}
	if strings.TrimSpace(c.DefaultModel) == "" {
		return fmt.Errorf("default_model must not be empty")
	}
	if c.UpstreamBaseURL == "" || !strings.HasPrefix(c.UpstreamBaseURL, "http") {
		return fmt.Errorf("upstream_base_url must be an http(s) URL")
	}
	if !c.PermissiveAuth && len(c.APIKeys) == 0 {
		return fmt.Errorf("api_keys must contain at least one key when permissive_auth is false")
	}
	return nil
}

// DefaultPath resolves the config location.
// It prioritizes a local config.json in the current project or next to the executable
// before falling back to the user config directory (AppData / .config).
func DefaultPath() string {
	if p := strings.TrimSpace(os.Getenv(EnvConfigPath)); p != "" {
		return p
	}
	// 1. Current working directory: ./config.json
	if info, err := os.Stat("config.json"); err == nil && !info.IsDir() {
		if abs, err := filepath.Abs("config.json"); err == nil {
			return abs
		}
		return "config.json"
	}
	// 2. Parent directory (e.g. when run from bin/): ../config.json
	parentConfig := filepath.Join("..", "config.json")
	if info, err := os.Stat(parentConfig); err == nil && !info.IsDir() {
		if abs, err := filepath.Abs(parentConfig); err == nil {
			return abs
		}
	}
	// 3. Next to executable or executable's parent
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidate1 := filepath.Join(exeDir, "config.json")
		if info, err := os.Stat(candidate1); err == nil && !info.IsDir() {
			return candidate1
		}
		candidate2 := filepath.Join(exeDir, "..", "config.json")
		if info, err := os.Stat(candidate2); err == nil && !info.IsDir() {
			if abs, err := filepath.Abs(candidate2); err == nil {
				return abs
			}
		}
	}
	// 4. Global system / user config directory fallback (AppData / ~/.config)
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "opencode-proxy", "config.json")
	}
	return filepath.Join(".", "config.json")
}

// Manager owns the live configuration and persists changes atomically.
type Manager struct {
	mu        sync.RWMutex
	path      string
	cfg       Config
	listeners []func(Config)
}

// NewManager loads (or creates) the config file at path.
func NewManager(path string) (*Manager, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	m := &Manager{path: path, cfg: Default()}

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		cfg := Default()
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg.Normalize()
		m.cfg = cfg
	case os.IsNotExist(err):
		if err := m.persist(m.cfg); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	m.applyEnvOverrides()
	if err := m.cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return m, nil
}

// Path returns the resolved config file path.
func (m *Manager) Path() string { return m.path }

// Get returns a deep copy of the live configuration.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Clone()
}

// Update validates, persists and publishes a new configuration.
func (m *Manager) Update(next Config) error {
	next.Normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if err := m.persist(next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.cfg = next
	listeners := append([]func(Config){}, m.listeners...)
	m.mu.Unlock()

	for _, fn := range listeners {
		fn(next)
	}
	return nil
}

// Configure binds a callback invoked immediately and after every update.
func (m *Manager) Configure(fn func(Config)) {
	m.mu.Lock()
	m.listeners = append(m.listeners, fn)
	cfg := m.cfg
	m.mu.Unlock()
	fn(cfg)
}

func (m *Manager) applyEnvOverrides() {
	if v := strings.TrimSpace(os.Getenv(EnvHost)); v != "" {
		m.cfg.Host = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvPort)); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 && port < 65536 {
			m.cfg.Port = port
		}
	}
}

// persist writes the file atomically (temp file + rename) so a crash can never
// leave a half-written config.json behind.
func (m *Manager) persist(cfg Config) error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	blob = append(blob, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// GetPuterAccounts returns all configured Puter accounts sorted by priority.
func (m *Manager) GetPuterAccounts() []PuterAccount {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]PuterAccount, len(m.cfg.PuterAccounts))
	copy(res, m.cfg.PuterAccounts)
	sort.SliceStable(res, func(i, j int) bool {
		return res[i].Priority < res[j].Priority
	})
	return res
}

// GetActivePuterAccountCandidates returns all candidate accounts ready for traffic,
// auto-clearing expired cooldowns, sorted by priority.
func (m *Manager) GetActivePuterAccountCandidates() []PuterAccount {
	m.mu.Lock()
	now := time.Now()
	var mutated bool
	for i := range m.cfg.PuterAccounts {
		acc := &m.cfg.PuterAccounts[i]
		if (acc.Status == "cooldown" || acc.Status == "rate_limited") && acc.CooldownUntil != nil && now.After(*acc.CooldownUntil) {
			acc.Status = "active"
			acc.CooldownUntil = nil
			mutated = true
		}
	}
	if mutated {
		_ = m.persist(m.cfg)
	}

	var candidates []PuterAccount
	for _, acc := range m.cfg.PuterAccounts {
		if acc.Status == "active" && acc.Token != "" {
			candidates = append(candidates, acc)
		}
	}
	m.mu.Unlock()

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})
	return candidates
}

// MarkPuterAccountStatus updates status, cooldown duration, and error message for an account.
func (m *Manager) MarkPuterAccountStatus(accountID string, status string, cooldownDuration time.Duration, lastErr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.cfg.PuterAccounts {
		if m.cfg.PuterAccounts[i].ID == accountID {
			m.cfg.PuterAccounts[i].Status = status
			m.cfg.PuterAccounts[i].LastError = lastErr
			if cooldownDuration > 0 {
				until := time.Now().Add(cooldownDuration)
				m.cfg.PuterAccounts[i].CooldownUntil = &until
			} else {
				m.cfg.PuterAccounts[i].CooldownUntil = nil
			}
			now := time.Now()
			m.cfg.PuterAccounts[i].LastUsedAt = &now
			_ = m.persist(m.cfg)
			break
		}
	}
}

// RecordPuterAccountUsage marks last used time for an account.
func (m *Manager) RecordPuterAccountUsage(accountID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.cfg.PuterAccounts {
		if m.cfg.PuterAccounts[i].ID == accountID {
			now := time.Now()
			m.cfg.PuterAccounts[i].LastUsedAt = &now
			_ = m.persist(m.cfg)
			break
		}
	}
}

// AddOrUpdatePuterAccount adds a new account or updates an existing one by ID or Username.
func (m *Manager) AddOrUpdatePuterAccount(acc PuterAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	acc.Token = strings.TrimSpace(acc.Token)
	if acc.Token == "" {
		return fmt.Errorf("token cannot be empty")
	}
	if strings.TrimSpace(acc.Username) == "" {
		acc.Username = "user_" + fmt.Sprintf("%d", time.Now().Unix()%10000)
	}
	if strings.TrimSpace(acc.ID) == "" {
		acc.ID = "acc_" + fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	}
	if acc.Status == "" {
		acc.Status = "active"
	}

	found := false
	for i := range m.cfg.PuterAccounts {
		if m.cfg.PuterAccounts[i].ID == acc.ID || (acc.Username != "" && m.cfg.PuterAccounts[i].Username == acc.Username) {
			m.cfg.PuterAccounts[i].Token = acc.Token
			m.cfg.PuterAccounts[i].Username = acc.Username
			if acc.Status != "" {
				m.cfg.PuterAccounts[i].Status = acc.Status
			}
			if acc.Priority > 0 {
				m.cfg.PuterAccounts[i].Priority = acc.Priority
			}
			found = true
			break
		}
	}

	if !found {
		if acc.Priority <= 0 {
			acc.Priority = len(m.cfg.PuterAccounts) + 1
		}
		m.cfg.PuterAccounts = append(m.cfg.PuterAccounts, acc)
	}

	if len(m.cfg.PuterAccounts) > 0 {
		m.cfg.PuterAPIKey = m.cfg.PuterAccounts[0].Token
	}

	return m.persist(m.cfg)
}

// DeletePuterAccount removes an account by ID.
func (m *Manager) DeletePuterAccount(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var updated []PuterAccount
	for _, acc := range m.cfg.PuterAccounts {
		if acc.ID != accountID {
			updated = append(updated, acc)
		}
	}
	m.cfg.PuterAccounts = updated
	if len(m.cfg.PuterAccounts) > 0 {
		m.cfg.PuterAPIKey = m.cfg.PuterAccounts[0].Token
	} else {
		m.cfg.PuterAPIKey = ""
	}
	return m.persist(m.cfg)
}

// SetPuterAccountPriority changes priority for an account and re-normalizes priorities.
func (m *Manager) SetPuterAccountPriority(accountID string, priority int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetIdx := -1
	for i, acc := range m.cfg.PuterAccounts {
		if acc.ID == accountID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return fmt.Errorf("account not found: %s", accountID)
	}

	m.cfg.PuterAccounts[targetIdx].Priority = priority
	sort.SliceStable(m.cfg.PuterAccounts, func(i, j int) bool {
		return m.cfg.PuterAccounts[i].Priority < m.cfg.PuterAccounts[j].Priority
	})
	for i := range m.cfg.PuterAccounts {
		m.cfg.PuterAccounts[i].Priority = i + 1
	}
	if len(m.cfg.PuterAccounts) > 0 {
		m.cfg.PuterAPIKey = m.cfg.PuterAccounts[0].Token
	}
	return m.persist(m.cfg)
}

// ResetPuterAccount clears cooldown/rate-limit status for an account.
func (m *Manager) ResetPuterAccount(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.cfg.PuterAccounts {
		if m.cfg.PuterAccounts[i].ID == accountID {
			m.cfg.PuterAccounts[i].Status = "active"
			m.cfg.PuterAccounts[i].CooldownUntil = nil
			m.cfg.PuterAccounts[i].LastError = ""
			return m.persist(m.cfg)
		}
	}
	return fmt.Errorf("account not found: %s", accountID)
}
