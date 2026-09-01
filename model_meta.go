package main

import (
	"strings"
)

// ModelInfo holds metadata for a single model, returned by /v1/models.
type ModelInfo struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	Created       int64    `json:"created"`
	OwnedBy       string   `json:"owned_by"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ContextLength int      `json:"context_length"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Multimodal    bool     `json:"multimodal"`
	Streaming     bool     `json:"streaming"`
	Reasoning     bool     `json:"reasoning"`
	Tier          string   `json:"tier"`            // "standard" | "premium" | "limited"
	Premium       bool     `json:"premium"`
	Available     bool     `json:"available"`
	Tags          []string `json:"tags,omitempty"`
}

// modelMetadata is the static metadata table, keyed by model ID.
// Sources: Codebuff freebuff-model-ids.ts + freebuff-models.ts (2026-09-01).
var modelMetadata = map[string]ModelInfo{
	"mimo/mimo-v2.5": {
		Name: "MiMo 2.5", Description: "Xiaomi MiMo 2.5 — balanced multimodal model, current fallback default",
		ContextLength: 131072, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "standard", Premium: false, Available: true,
		Tags: []string{"free", "multimodal", "reasoning", "coding"},
	},
	"z-ai/glm-5.3-flash": {
		Name: "GLM 5.3 Flash", Description: "Zhipu GLM 5.3 Flash — recommended default model, fast and capable",
		ContextLength: 131072, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "standard", Premium: false, Available: true,
		Tags: []string{"free", "multimodal", "reasoning", "coding", "fast"},
	},
	"z-ai/glm-5.2": {
		Name: "GLM 5.2", Description: "Zhipu GLM 5.2 — unlock by referring friends",
		ContextLength: 131072, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "referral", Premium: false, Available: true,
		Tags: []string{"free", "referral", "multimodal"},
	},
	"openai/gpt-5.6-luna": {
		Name: "GPT-5.6 Luna", Description: "OpenAI GPT-5.6 Luna — premium model, 4 sessions/day",
		ContextLength: 1000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: true,
		Tags: []string{"premium", "gpt", "multimodal", "reasoning"},
	},
	"openai/gpt-5.6-luna-max": {
		Name: "GPT-5.6 Luna Max", Description: "GPT-5.6 Luna with extended context",
		ContextLength: 2000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: true,
		Tags: []string{"premium", "gpt", "extended-context"},
	},
	"openai/gpt-5.6-luna-es": {
		Name: "GPT-5.6 Luna ES", Description: "GPT-5.6 Luna variant",
		ContextLength: 1000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: true,
		Tags: []string{"premium", "gpt"},
	},
	"upstage/solar-pro4": {
		Name: "Solar Pro 4", Description: "Upstage Solar Pro 4 — experimental premium, zero data retention",
		ContextLength: 65536, Multimodal: false, Streaming: true, Reasoning: false,
		Tier: "premium", Premium: true, Available: true,
		Tags: []string{"premium", "experimental"},
	},
	"anthropic/claude-fable-5": {
		Name: "Claude Fable 5", Description: "Anthropic Claude Fable 5 — limited-time trial",
		ContextLength: 200000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "limited", Premium: false, Available: true,
		Tags: []string{"limited", "anthropic", "trial"},
	},
	"meta/muse-spark-1.2-contributor": {
		Name: "Muse Spark 1.2", Description: "Meta Muse Spark 1.2 Contributor — Web only, 60 RPM shared",
		ContextLength: 1000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "limited", Premium: false, Available: true,
		Tags: []string{"web-only", "meta", "rate-limited"},
	},
	"crof/kimi-k3-eco": {
		Name: "Kimi K3 Eco", Description: "Kimi K3 Eco — Web only",
		ContextLength: 131072, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "standard", Premium: false, Available: true,
		Tags: []string{"web-only", "free"},
	},
	"deepseek/deepseek-v4-flash": {
		Name: "DeepSeek V4 Flash", Description: "DeepSeek V4 Flash — paused from free mode 2026-08-18",
		ContextLength: 131072, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "deepseek"},
	},
	"deepseek/deepseek-v4-pro": {
		Name: "DeepSeek V4 Pro", Description: "DeepSeek V4 Pro — paused from free mode 2026-08-26",
		ContextLength: 131072, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "deepseek"},
	},
	"deepseek/deepseek-v4-flash-max": {
		Name: "DeepSeek V4 Flash Max", Description: "DeepSeek V4 Flash with extended context",
		ContextLength: 262144, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "deepseek", "extended-context"},
	},
	"deepseek/deepseek-v4-pro-max": {
		Name: "DeepSeek V4 Pro Max", Description: "DeepSeek V4 Pro with extended context",
		ContextLength: 262144, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "deepseek", "extended-context"},
	},
	"minimax/minimax-m3": {
		Name: "MiniMax M3", Description: "MiniMax M3 — paused from free mode 2026-08-20 (cost $213/hr)",
		ContextLength: 524288, Multimodal: false, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "minimax"},
	},
	"stealth/ox-alpha": {
		Name: "Ox Alpha", Description: "Anonymous stealth model — paused 2026-08-27 (host ended free promo)",
		ContextLength: 1000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: false,
		Tags: []string{"paused", "stealth"},
	},
	"google/gemini-3.1-pro-preview": {
		Name: "Gemini 3.1 Pro", Description: "Google Gemini 3.1 Pro Preview",
		ContextLength: 2000000, Multimodal: true, Streaming: true, Reasoning: true,
		Tier: "premium", Premium: true, Available: true,
		Tags: []string{"premium", "google", "multimodal"},
	},
}

// defaultAliases provides built-in short-name → full model ID mapping.
var defaultAliases = map[string]string{
	"auto":     "z-ai/glm-5.3-flash",
	"default":  "z-ai/glm-5.3-flash",
	"glm":      "z-ai/glm-5.3-flash",
	"glm-flash": "z-ai/glm-5.3-flash",
	"glm-52":   "z-ai/glm-5.2",
	"mimo":     "mimo/mimo-v2.5",
	"luna":     "openai/gpt-5.6-luna",
	"gpt":      "openai/gpt-5.6-luna",
	"flash":    "z-ai/glm-5.3-flash",
	"deepseek": "deepseek/deepseek-v4-flash",
	"fable":    "anthropic/claude-fable-5",
	"solar":    "upstage/solar-pro4",
	"kimi":     "crof/kimi-k3-eco",
}

// defaultFallbacks maps a model to fallback alternatives when it's unavailable.
var defaultFallbacks = map[string][]string{
	"openai/gpt-5.6-luna":         {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"openai/gpt-5.6-luna-es":      {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"openai/gpt-5.6-luna-max":     {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"upstage/solar-pro4":           {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"deepseek/deepseek-v4-flash":   {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"deepseek/deepseek-v4-pro":     {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"deepseek/deepseek-v4-flash-max": {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"deepseek/deepseek-v4-pro-max":   {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"minimax/minimax-m3":           {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"stealth/ox-alpha":             {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
	"anthropic/claude-fable-5":     {"z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"},
}

// DefaultFallbackModel is used when no fallback chain is configured.
const DefaultFallbackModel = "mimo/mimo-v2.5"

// resolveAlias converts a short alias to the full model ID.
// Checks user-configured aliases first, then built-in defaults.
func resolveAlias(alias string, userAliases map[string]string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ""
	}
	// User aliases take priority
	if userAliases != nil {
		if resolved, ok := userAliases[alias]; ok {
			return resolved
		}
	}
	// Built-in aliases
	if resolved, ok := defaultAliases[alias]; ok {
		return resolved
	}
	// Not an alias, return as-is
	return alias
}

// resolveFallback returns the fallback model ID for a given model.
// Checks user-configured fallbacks first, then built-in defaults.
func resolveFallback(model string, userFallbacks map[string][]string) string {
	if userFallbacks != nil {
		if chain, ok := userFallbacks[model]; ok && len(chain) > 0 {
			return chain[0]
		}
	}
	if chain, ok := defaultFallbacks[model]; ok && len(chain) > 0 {
		return chain[0]
	}
	return DefaultFallbackModel
}

// getModelMeta returns metadata for a model ID, with a sensible default.
func getModelMeta(modelID string) ModelInfo {
	if meta, ok := modelMetadata[modelID]; ok {
		// Override availability based on paused list
		if pausedFreeModels[modelID] {
			meta.Available = false
		}
		return meta
	}
	// Unknown model: return minimal metadata
	return ModelInfo{
		ID:            modelID,
		Name:          modelID,
		Description:   "",
		ContextLength: 131072,
		Multimodal:    false,
		Streaming:     true,
		Reasoning:     false,
		Tier:          "standard",
		Premium:       false,
		Available:     true,
	}
}
