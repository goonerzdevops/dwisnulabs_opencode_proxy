package proxy

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"opencode-proxy/internal/config"
)

// ModelEntry is one OpenAI-compatible /v1/models record.
type ModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelCatalog merges the statically known free-tier models with the upstream
// catalogue, cached briefly so a model picker refresh cannot hammer the gate.
type ModelCatalog struct {
	upstream *Upstream
	ttl      time.Duration

	mu        sync.Mutex
	cached    []ModelEntry
	fetchedAt time.Time
}

// NewModelCatalog builds a catalog backed by upstream.
func NewModelCatalog(upstream *Upstream) *ModelCatalog {
	return &ModelCatalog{upstream: upstream, ttl: 2 * time.Minute}
}

// List returns the merged catalogue, refreshing from upstream at most once per TTL.
func (c *ModelCatalog) List(ctx context.Context, cfg config.Config, fp Fingerprint, forceRefresh ...bool) []ModelEntry {
	force := len(forceRefresh) > 0 && forceRefresh[0]
	if cfg.FollowUpstreamModels {
		c.mu.Lock()
		stale := force || time.Since(c.fetchedAt) > c.ttl
		cached := c.cached
		c.mu.Unlock()

		if stale {
			if ids, err := c.upstream.FetchModels(ctx, cfg, fp); err == nil {
				entries := make([]ModelEntry, 0, len(ids))
				now := time.Now().Unix()
				for _, id := range ids {
					entries = append(entries, ModelEntry{ID: id, Object: "model", Created: now, OwnedBy: "opencode"})
				}
				c.mu.Lock()
				c.cached = entries
				c.fetchedAt = time.Now()
				c.mu.Unlock()
				cached = entries
			} else {
				// Serve the static list rather than an empty catalogue, but retry
				// soon so a transient gate failure does not stick for the TTL.
				c.mu.Lock()
				c.fetchedAt = time.Now().Add(-(c.ttl - time.Minute))
				c.mu.Unlock()
			}
		}
		if len(cached) > 0 {
			return mergeModels(staticModels(cfg), cached)
		}
	}
	return staticModels(cfg)
}

// staticModels is the always-available catalogue: real upstream ids plus every
// alias, so clients that request gpt-4o/claude-* can also validate the name.
var knownActiveModels = []string{
	"big-pickle",
	"mimo-v2.6-flash-free",
	"nemotron-3.5-lightning-free",
	"glm-5.3-flash-free",
	"beam-free",
	"flint-40b-free",
	"nemotron-3.5-super-vl-free",
	"jev-1.13-free",
}

func staticModels(cfg config.Config) []ModelEntry {
	now := time.Now().Unix()
	ids := map[string]bool{cfg.DefaultModel: true}
	for _, id := range knownActiveModels {
		ids[id] = true
	}
	for name := range responsesModels {
		ids[name] = true
	}
	for name := range messagesModels {
		ids[name] = true
	}
	for alias := range cfg.ModelAliases {
		ids[alias] = true
	}

	entries := make([]ModelEntry, 0, len(ids))
	for id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		entries = append(entries, ModelEntry{ID: id, Object: "model", Created: now, OwnedBy: "opencode"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

// mergeModels unions two catalogues, preferring primary entries.
func mergeModels(primary, secondary []ModelEntry) []ModelEntry {
	seen := map[string]bool{}
	out := make([]ModelEntry, 0, len(primary)+len(secondary))
	for _, list := range [][]ModelEntry{primary, secondary} {
		for _, entry := range list {
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
