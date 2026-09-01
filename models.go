package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	freeAgentsSourceURL  = "https://raw.githubusercontent.com/CodebuffAI/codebuff/main/common/src/constants/free-agents.ts"
	modelRefreshInterval = 6 * time.Hour
	rootAgentID          = "base2-free"
)

// hardcodedFallback 为内置模型注册表。所有子代理均在请求时挂载在根 agent (base2-free) 之下。
var hardcodedFallback = map[string][]string{
	"file-picker":                 {"google/gemini-2.5-flash-lite"},
	"file-picker-max":             {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview", "google/gemini-3-flash-preview"},
	"file-lister":                 {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview"},
	"researcher-web":              {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview"},
	"researcher-docs":             {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview"},
	"basher":                      {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview"},
	"browser-use":                 {"google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite-preview"},
	"code-reviewer-mimo":          {"mimo/mimo-v2.5"},
}

// pausedFreeModels lists models withdrawn from free mode by Codebuff.
// Requests for these models are silently coerced to the fallback by the upstream.
// Updated 2026-09-01.
var pausedFreeModels = map[string]bool{
	"minimax/minimax-m3":       true, // paused 2026-08-20, cost $213/hr
	"deepseek/deepseek-v4-pro":  true, // paused 2026-08-26, cost too high
	"stealth/ox-alpha":         true, // paused 2026-08-27, host ended free promo
}

// precompiled regexes for parsing free-agents.ts
var (
	blockPattern = regexp.MustCompile(`'([^']+)':\s*new\s+Set\(\[([^\]]*)\]\)`)
	modelPattern = regexp.MustCompile(`'([^']+)'`)
)

// ModelRegistry fetches and caches the agent→model mapping for all free agents
// from the upstream free-agents.ts source file.
type ModelRegistry struct {
	client *http.Client
	logger *log.Logger

	mu           sync.RWMutex
	agentModels  map[string][]string // agentID → []model
	modelToAgent map[string]string   // model → chosen agentID
	allModels    []string            // deduplicated, sorted
	lastOK       time.Time

	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewModelRegistry(client *http.Client, logger *log.Logger) *ModelRegistry {
	return &ModelRegistry{
		client:       client,
		logger:       logger,
		agentModels:  make(map[string][]string),
		modelToAgent: make(map[string]string),
		stopCh:       make(chan struct{}),
	}
}

func (r *ModelRegistry) Start(ctx context.Context) {
	if err := r.refresh(ctx); err != nil {
		r.logger.Printf("model registry: initial fetch failed, loading hardcoded fallback: %v", err)
		r.loadFallback()
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(modelRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := r.refresh(ctx); err != nil {
					r.logger.Printf("model registry: refresh failed: %v", err)
				}
				cancel()
			case <-r.stopCh:
				return
			}
		}
	}()
}

func (r *ModelRegistry) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

// Models returns the deduplicated list of all available model names.
func (r *ModelRegistry) Models() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.allModels))
	copy(out, r.allModels)
	return out
}

// HasModel checks if the given model is available (not paused).
func (r *ModelRegistry) HasModel(model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.modelToAgent[model]; !ok {
		return false
	}
	return !pausedFreeModels[model]
}

// IsPausedModel returns true if the model is known but paused by upstream.
func (r *ModelRegistry) IsPausedModel(model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, known := r.modelToAgent[model]
	return known && pausedFreeModels[model]
}

// ResolveModelAndAgent maps any model ID (including common aliases like gemini-2.5-flash-lite, gpt-4o, claude-3-5-sonnet, etc.)
// to the actual upstream model ID and serving agent ID.
func (r *ModelRegistry) ResolveModelAndAgent(model string) (targetModel string, agentID string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. Direct match
	if agent, exists := r.modelToAgent[model]; exists {
		return model, agent, true
	}

	// 2. Alias match
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(normalized, "mimo"):
		return "mimo/mimo-v2.5", "code-reviewer-mimo", true
	case strings.Contains(normalized, "3.5"):
		return "google/gemini-3.5-flash-lite", "file-picker-max", true
	case strings.Contains(normalized, "3.1") || strings.Contains(normalized, "gemini-3"):
		return "google/gemini-3.1-flash-lite-preview", "file-picker-max", true
	case strings.Contains(normalized, "gemini") || strings.Contains(normalized, "flash-lite"):
		return "google/gemini-2.5-flash-lite", "file-picker", true
	default:
		// Default fallback for generic names (e.g. gpt-4o, claude-3-5-sonnet, default)
		if agent, exists := r.modelToAgent["google/gemini-2.5-flash-lite"]; exists {
			return "google/gemini-2.5-flash-lite", agent, true
		}
		return "google/gemini-2.5-flash-lite", "file-picker", true
	}
}

// AgentForModel returns the agent ID that should serve the given model.
func (r *ModelRegistry) AgentForModel(model string) (string, bool) {
	_, agent, ok := r.ResolveModelAndAgent(model)
	return agent, ok
}

// AgentIDs returns the list of all known agent IDs.
func (r *ModelRegistry) AgentIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.agentModels))
	for id := range r.agentModels {
		ids = append(ids, id)
	}
	return ids
}

func (r *ModelRegistry) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, freeAgentsSourceURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch free-agents source: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	all := parseAllFreeModels(string(body))
	if len(all) == 0 {
		return fmt.Errorf("no free agents found in source")
	}

	// Validate that the parsed result contains root agents (base2-free*).
	// If the upstream file format changes and only subagents are parsed,
	// fall back to the hardcoded list instead of using an incomplete result.
	hasRoot := false
	for agentID := range all {
		if strings.HasPrefix(agentID, "base2-free") {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return fmt.Errorf("parsed agents do not contain any base2-free root agents (upstream format may have changed)")
	}

	modelToAgent, allModels := buildModelMapping(all)

	r.mu.Lock()
	r.agentModels = all
	r.modelToAgent = modelToAgent
	r.allModels = allModels
	r.lastOK = time.Now()
	r.mu.Unlock()

	r.logger.Printf("model registry: updated %d agents, %d models: %v", len(all), len(allModels), allModels)
	return nil
}

func (r *ModelRegistry) loadFallback() {
	modelToAgent, allModels := buildModelMapping(hardcodedFallback)

	r.mu.Lock()
	r.agentModels = hardcodedFallback
	r.modelToAgent = modelToAgent
	r.allModels = allModels
	r.mu.Unlock()

	r.logger.Printf("model registry: loaded fallback models: %v", allModels)
}

// parseAllFreeModels extracts ALL agent→models mappings from the free-agents.ts source.
func parseAllFreeModels(source string) map[string][]string {
	result := make(map[string][]string)
	for _, match := range blockPattern.FindAllStringSubmatch(source, -1) {
		agentID := match[1]
		modelsStr := match[2]

		var models []string
		for _, modelMatch := range modelPattern.FindAllStringSubmatch(modelsStr, -1) {
			model := strings.TrimSpace(modelMatch[1])
			if model != "" {
				models = append(models, model)
			}
		}
		if len(models) > 0 {
			result[agentID] = models
		}
	}
	return result
}

// buildModelMapping creates the model→agent reverse mapping and deduplicated model list.
// When a model appears in multiple agents, the most specific agent (fewest models)
// is chosen to ensure exact agent↔model pairing for free-mode validation.
func buildModelMapping(agentModels map[string][]string) (map[string]string, []string) {
	modelAgents := make(map[string][]string)
	for agentID, models := range agentModels {
		for _, model := range models {
			modelAgents[model] = append(modelAgents[model], agentID)
		}
	}

	modelToAgent := make(map[string]string, len(modelAgents))
	allModels := make([]string, 0, len(modelAgents))
	for model, agents := range modelAgents {
		// Pick the most specific agent: the one that handles the fewest models.
		// This ensures e.g. "base2-free-glm-5-3-flash" is preferred over "base2-free"
		// for model "z-ai/glm-5.3-flash".
		best := agents[0]
		bestCount := len(agentModels[best])
		for _, a := range agents[1:] {
			if c := len(agentModels[a]); c < bestCount {
				best = a
				bestCount = c
			}
		}
		modelToAgent[model] = best
		allModels = append(allModels, model)
	}
	sort.Strings(allModels)
	return modelToAgent, allModels
}
