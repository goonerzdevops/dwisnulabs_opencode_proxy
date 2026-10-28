package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTelemetryStoreRecordAndSummary(t *testing.T) {
	tempDir := t.TempDir()
	walPath := filepath.Join(tempDir, "test_usage.jsonl")

	ts := NewTelemetryStore(10, walPath)
	defer ts.Close()

	rec1 := RequestRecord{
		ID:             "req-1",
		Timestamp:      time.Now(),
		RequestedModel: "gpt-4o",
		ActualModel:    "deepseek-v4.1-flash-free",
		Lane:           "chat",
		Stream:         true,
		PromptTokens:   1000,
		CachedTokens:   800,
		OutputTokens:   200,
		DurationMs:     500,
		StatusCode:     200,
	}

	ts.Record(rec1)

	// Wait for WAL channel to flush
	time.Sleep(50 * time.Millisecond)

	summary := ts.Summary(10)
	if summary.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", summary.TotalRequests)
	}
	if summary.TotalPromptTokens != 1000 {
		t.Fatalf("expected 1000 prompt tokens, got %d", summary.TotalPromptTokens)
	}
	if summary.TotalCachedTokens != 800 {
		t.Fatalf("expected 800 cached tokens, got %d", summary.TotalCachedTokens)
	}
	if summary.TotalOutputTokens != 200 {
		t.Fatalf("expected 200 output tokens, got %d", summary.TotalOutputTokens)
	}
	if summary.OverallCacheHitPct != 80.0 {
		t.Fatalf("expected 80.0%% cache hit rate, got %f", summary.OverallCacheHitPct)
	}
	if len(summary.RecentRecords) != 1 {
		t.Fatalf("expected 1 recent record, got %d", len(summary.RecentRecords))
	}
	if summary.RecentRecords[0].CacheHitRate != 80.0 {
		t.Fatalf("expected 80.0%% record hit rate, got %f", summary.RecentRecords[0].CacheHitRate)
	}

	// Verify WAL re-read on new store instance
	ts2 := NewTelemetryStore(10, walPath)
	defer ts2.Close()

	s2 := ts2.Summary(10)
	if s2.TotalRequests != 1 || s2.TotalCachedTokens != 800 {
		t.Fatalf("expected WAL recovery of 1 req and 800 cached tokens, got %d and %d", s2.TotalRequests, s2.TotalCachedTokens)
	}

	// Verify Clear flushes memory and WAL
	ts2.Clear(true)
	s3 := ts2.Summary(10)
	if s3.TotalRequests != 0 || s3.TotalPromptTokens != 0 || len(s3.RecentRecords) != 0 {
		t.Fatalf("expected 0 requests and 0 records after clear, got req=%d, recs=%d", s3.TotalRequests, len(s3.RecentRecords))
	}

	_ = os.Remove(walPath)
}
