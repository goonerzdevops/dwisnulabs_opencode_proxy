package proxy

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// RequestRecord captures telemetry for a single completed proxy request.
type RequestRecord struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	RequestedModel string    `json:"requested_model"`
	ActualModel    string    `json:"actual_model"`
	Lane           string    `json:"lane"`
	Stream         bool      `json:"stream"`
	PromptTokens   int       `json:"prompt_tokens"`
	CachedTokens   int       `json:"cached_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	CacheHitRate   float64   `json:"cache_hit_rate"` // 0.0 - 100.0 %
	DurationMs     int64     `json:"duration_ms"`
	StatusCode     int       `json:"status_code"`
}

// TelemetrySummary contains overall cumulative metrics and recent request history.
type TelemetrySummary struct {
	TotalRequests      int64           `json:"total_requests"`
	TotalPromptTokens  int64           `json:"total_prompt_tokens"`
	TotalCachedTokens  int64           `json:"total_cached_tokens"`
	TotalOutputTokens  int64           `json:"total_output_tokens"`
	OverallCacheHitPct float64         `json:"overall_cache_hit_pct"`
	RecentRecords      []RequestRecord `json:"recent_records"`
}

// TelemetryStore maintains thread-safe token metrics in-memory (ring buffer)
// and asynchronously appends to a WAL JSONL file.
type TelemetryStore struct {
	mu          sync.RWMutex
	records     []RequestRecord
	capacity    int
	cursor      int
	count       int
	walPath     string
	walCh       chan RequestRecord
	closeOnce   sync.Once
	doneCh      chan struct{}

	totalRequests     atomic.Int64
	totalPromptTokens atomic.Int64
	totalCachedTokens atomic.Int64
	totalOutputTokens atomic.Int64
}

// NewTelemetryStore initializes the in-memory ring buffer and starts the WAL worker.
func NewTelemetryStore(capacity int, walPath string) *TelemetryStore {
	if capacity <= 0 {
		capacity = 200
	}
	ts := &TelemetryStore{
		records:   make([]RequestRecord, capacity),
		capacity:  capacity,
		walPath:   walPath,
		walCh:     make(chan RequestRecord, 256),
		doneCh:    make(chan struct{}),
	}

	ts.loadWAL()
	go ts.walWriter()
	return ts
}

// Record inserts a new request record into the ring buffer, updates totals, and queues for WAL append.
func (ts *TelemetryStore) Record(rec RequestRecord) {
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	if rec.TotalTokens == 0 {
		rec.TotalTokens = rec.PromptTokens + rec.OutputTokens
	}
	if rec.PromptTokens > 0 {
		rec.CacheHitRate = math.Round((float64(rec.CachedTokens)/float64(rec.PromptTokens))*10000) / 100
	}

	ts.totalRequests.Add(1)
	ts.totalPromptTokens.Add(int64(rec.PromptTokens))
	ts.totalCachedTokens.Add(int64(rec.CachedTokens))
	ts.totalOutputTokens.Add(int64(rec.OutputTokens))

	ts.mu.Lock()
	ts.records[ts.cursor] = rec
	ts.cursor = (ts.cursor + 1) % ts.capacity
	if ts.count < ts.capacity {
		ts.count++
	}
	ts.mu.Unlock()

	// Non-blocking queue to WAL worker
	select {
	case ts.walCh <- rec:
	default:
		// Queue full, drop disk write to prevent backpressure on proxy stream
	}
}

// Summary returns cumulative totals and the latest N records (newest first).
func (ts *TelemetryStore) Summary(limit int) TelemetrySummary {
	if limit <= 0 || limit > ts.capacity {
		limit = 50
	}

	prompt := ts.totalPromptTokens.Load()
	cached := ts.totalCachedTokens.Load()
	output := ts.totalOutputTokens.Load()
	reqs := ts.totalRequests.Load()

	var hitPct float64
	if prompt > 0 {
		hitPct = math.Round((float64(cached)/float64(prompt))*10000) / 100
	}

	ts.mu.RLock()
	available := ts.count
	if available > limit {
		available = limit
	}
	recent := make([]RequestRecord, 0, available)
	for i := 0; i < available; i++ {
		idx := (ts.cursor - 1 - i + ts.capacity) % ts.capacity
		recent = append(recent, ts.records[idx])
	}
	ts.mu.RUnlock()

	return TelemetrySummary{
		TotalRequests:      reqs,
		TotalPromptTokens:  prompt,
		TotalCachedTokens:  cached,
		TotalOutputTokens:  output,
		OverallCacheHitPct: hitPct,
		RecentRecords:      recent,
	}
}

// TotalPromptTokens returns cumulative input tokens.
func (ts *TelemetryStore) TotalPromptTokens() int64 {
	return ts.totalPromptTokens.Load()
}

// TotalCachedTokens returns cumulative cached input tokens.
func (ts *TelemetryStore) TotalCachedTokens() int64 {
	return ts.totalCachedTokens.Load()
}

// TotalOutputTokens returns cumulative output tokens.
func (ts *TelemetryStore) TotalOutputTokens() int64 {
	return ts.totalOutputTokens.Load()
}

// OverallCacheHitPct returns overall cache hit rate percentage.
func (ts *TelemetryStore) OverallCacheHitPct() float64 {
	prompt := ts.totalPromptTokens.Load()
	if prompt <= 0 {
		return 0
	}
	return math.Round((float64(ts.totalCachedTokens.Load())/float64(prompt))*10000) / 100
}

// Clear flushes all in-memory records and resets cumulative counters.
// If resetWAL is true, it also truncates the on-disk usage.jsonl file.
func (ts *TelemetryStore) Clear(resetWAL bool) {
	ts.mu.Lock()
	ts.records = make([]RequestRecord, ts.capacity)
	ts.cursor = 0
	ts.count = 0
	ts.mu.Unlock()

	ts.totalRequests.Store(0)
	ts.totalPromptTokens.Store(0)
	ts.totalCachedTokens.Store(0)
	ts.totalOutputTokens.Store(0)

	if resetWAL && ts.walPath != "" {
		_ = os.Truncate(ts.walPath, 0)
	}
}


// walWriter processes queued records and appends them to disk.
func (ts *TelemetryStore) walWriter() {
	defer close(ts.doneCh)
	if ts.walPath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(ts.walPath), 0755)

	f, err := os.OpenFile(ts.walPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	for rec := range ts.walCh {
		blob, err := json.Marshal(rec)
		if err == nil {
			_, _ = f.Write(append(blob, '\n'))
		}
	}
}

// loadWAL replays the usage.jsonl file on startup to restore historical counts.
func (ts *TelemetryStore) loadWAL() {
	if ts.walPath == "" {
		return
	}
	f, err := os.Open(ts.walPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var records []RequestRecord
	var promptSum, cachedSum, outputSum int64
	var reqCount int64

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r RequestRecord
		if err := json.Unmarshal(line, &r); err == nil {
			reqCount++
			promptSum += int64(r.PromptTokens)
			cachedSum += int64(r.CachedTokens)
			outputSum += int64(r.OutputTokens)
			records = append(records, r)
		}
	}

	ts.totalRequests.Store(reqCount)
	ts.totalPromptTokens.Store(promptSum)
	ts.totalCachedTokens.Store(cachedSum)
	ts.totalOutputTokens.Store(outputSum)

	// Keep last N records in ring buffer
	if len(records) > 0 {
		start := 0
		if len(records) > ts.capacity {
			start = len(records) - ts.capacity
		}
		for _, rec := range records[start:] {
			ts.records[ts.cursor] = rec
			ts.cursor = (ts.cursor + 1) % ts.capacity
			if ts.count < ts.capacity {
				ts.count++
			}
		}
	}
}

// Close flushes the WAL channel and waits for writer to finish.
func (ts *TelemetryStore) Close() {
	ts.closeOnce.Do(func() {
		close(ts.walCh)
		<-ts.doneCh
	})
}
