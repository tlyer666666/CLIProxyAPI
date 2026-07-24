package tui

import (
	"strings"
	"testing"
)

func TestUsageTokenTotalPrefersTokenUsed(t *testing.T) {
	got := usageTokenTotal(map[string]any{
		"token_used":   float64(42),
		"total_tokens": float64(30),
	})
	if got != 42 {
		t.Fatalf("usageTokenTotal() = %d, want 42", got)
	}
}

func TestUsageTokenTotalFallsBackToTotalTokens(t *testing.T) {
	got := usageTokenTotal(map[string]any{
		"total_tokens": float64(30),
	})
	if got != 30 {
		t.Fatalf("usageTokenTotal() = %d, want 30", got)
	}
}

func TestRenderLatencyBreakdown(t *testing.T) {
	modelStats := map[string]any{
		"details": []any{
			map[string]any{"latency_ms": float64(100)},
			map[string]any{"latency_ms": float64(200)},
			map[string]any{"latency_ms": float64(300)},
		},
	}

	result := usageTabModel{}.renderLatencyBreakdown(modelStats)
	if !strings.Contains(result, "avg 200ms  min 100ms  max 300ms") {
		t.Fatalf("renderLatencyBreakdown() = %q", result)
	}
}
