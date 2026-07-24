package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesTokenUsedAndLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Provider:    "openai",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		TTFT:        250 * time.Millisecond,
		Generate:    coreusage.GenerateFlag(true),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	if snapshot.TotalTokens != 30 || snapshot.TokenUsed != 30 {
		t.Fatalf("snapshot tokens = total:%d token_used:%d, want 30/30", snapshot.TotalTokens, snapshot.TokenUsed)
	}
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 || details[0].TTFTMs != 250 {
		t.Fatalf("latency/ttft = %d/%d, want 1500/250", details[0].LatencyMs, details[0].TTFTMs)
	}
	if details[0].Tokens.TotalTokens != 30 || details[0].Tokens.TokenUsed != 30 {
		t.Fatalf("detail tokens = %+v, want total/token_used 30", details[0].Tokens)
	}
}

func TestRequestStatisticsNormalizesProviderTokenAccounting(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		detail     coreusage.Detail
		wantTokens int64
	}{
		{
			name:     "openai subset reasoning already in output",
			provider: "openai",
			detail: coreusage.Detail{
				InputTokens:     100,
				OutputTokens:    30,
				ReasoningTokens: 12,
			},
			wantTokens: 130,
		},
		{
			name:     "gemini separate reasoning",
			provider: "gemini",
			detail: coreusage.Detail{
				InputTokens:     100,
				OutputTokens:    30,
				ReasoningTokens: 12,
			},
			wantTokens: 142,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewRequestStatistics()
			stats.Record(context.Background(), coreusage.Record{
				Provider: tt.provider,
				Model:    "model",
				Detail:   tt.detail,
			})

			snapshot := stats.Snapshot()
			if snapshot.TotalTokens != tt.wantTokens || snapshot.TokenUsed != tt.wantTokens {
				t.Fatalf("tokens = total:%d token_used:%d, want %d", snapshot.TotalTokens, snapshot.TokenUsed, tt.wantTokens)
			}
		})
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}
}

func TestRequestStatisticsMergeSnapshotUsesTokenUsedFallback(t *testing.T) {
	stats := NewRequestStatistics()
	result := stats.MergeSnapshot(StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: time.Date(2026, 4, 25, 8, 0, 0, 0, time.UTC),
							Tokens: TokenStats{
								TokenUsed: 44,
							},
						}},
					},
				},
			},
		},
	})
	if result.Added != 1 {
		t.Fatalf("merge added = %d, want 1", result.Added)
	}
	snapshot := stats.Snapshot()
	if snapshot.TotalTokens != 44 || snapshot.TokenUsed != 44 {
		t.Fatalf("snapshot tokens = total:%d used:%d, want 44", snapshot.TotalTokens, snapshot.TokenUsed)
	}
}
