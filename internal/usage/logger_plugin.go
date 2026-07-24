// Package usage collects in-memory request and token statistics.
package usage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

var statisticsEnabled atomic.Bool

func init() {
	statisticsEnabled.Store(true)
	coreusage.RegisterPlugin(NewLoggerPlugin())
}

// LoggerPlugin aggregates usage records published by the runtime.
type LoggerPlugin struct {
	stats *RequestStatistics
}

// NewLoggerPlugin constructs a logger plugin backed by the shared stats store.
func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{stats: defaultRequestStatistics} }

// HandleUsage implements coreusage.Plugin.
func (p *LoggerPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if !statisticsEnabled.Load() {
		return
	}
	if p == nil || p.stats == nil {
		return
	}
	p.stats.Record(ctx, record)
}

// SetStatisticsEnabled toggles whether in-memory statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports whether in-memory statistics are recorded.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// RequestStatistics maintains aggregated request metrics in memory.
type RequestStatistics struct {
	mu sync.RWMutex

	totalRequests int64
	successCount  int64
	failureCount  int64
	totalTokens   int64

	apis map[string]*apiStats

	requestsByDay  map[string]int64
	requestsByHour map[int]int64
	tokensByDay    map[string]int64
	tokensByHour   map[int]int64
}

type apiStats struct {
	TotalRequests int64
	TotalTokens   int64
	Models        map[string]*modelStats
}

type modelStats struct {
	TotalRequests int64
	TotalTokens   int64
	Details       []RequestDetail
}

// RequestDetail stores per-request usage details.
type RequestDetail struct {
	Timestamp       time.Time        `json:"timestamp"`
	LatencyMs       int64            `json:"latency_ms"`
	TTFTMs          int64            `json:"ttft_ms"`
	Source          string           `json:"source"`
	AuthIndex       string           `json:"auth_index"`
	Tokens          TokenStats       `json:"tokens"`
	Failed          bool             `json:"failed"`
	Generate        bool             `json:"generate"`
	Fail            FailureSnapshot  `json:"fail,omitempty"`
	Provider        string           `json:"provider,omitempty"`
	ExecutorType    string           `json:"executor_type,omitempty"`
	Alias           string           `json:"alias,omitempty"`
	Endpoint        string           `json:"endpoint,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	ServiceTier     string           `json:"service_tier,omitempty"`
	ResponseTier    string           `json:"response_service_tier,omitempty"`
	TokenBreakdown  coreusage.TokenBreakdown `json:"token_breakdown,omitempty"`
}

// TokenStats captures the token usage breakdown for a request.
type TokenStats struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	TokenUsed           int64 `json:"token_used"`
}

// FailureSnapshot captures failure metadata stored in the statistics snapshot.
type FailureSnapshot struct {
	StatusCode int    `json:"status_code,omitempty"`
	Body       string `json:"body,omitempty"`
}

// StatisticsSnapshot represents an immutable view of aggregated metrics.
type StatisticsSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`
	TokenUsed     int64 `json:"token_used"`

	APIs map[string]APISnapshot `json:"apis"`

	RequestsByDay  map[string]int64 `json:"requests_by_day"`
	RequestsByHour map[string]int64 `json:"requests_by_hour"`
	TokensByDay    map[string]int64 `json:"tokens_by_day"`
	TokensByHour   map[string]int64 `json:"tokens_by_hour"`
}

// APISnapshot summarises metrics for a single API/source key.
type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	TotalTokens   int64                    `json:"total_tokens"`
	TokenUsed     int64                    `json:"token_used"`
	Models        map[string]ModelSnapshot `json:"models"`
}

// ModelSnapshot summarises metrics for a specific model.
type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	TotalTokens   int64           `json:"total_tokens"`
	TokenUsed     int64           `json:"token_used"`
	Details       []RequestDetail `json:"details"`
}

var defaultRequestStatistics = NewRequestStatistics()

// GetRequestStatistics returns the shared statistics store.
func GetRequestStatistics() *RequestStatistics { return defaultRequestStatistics }

// NewRequestStatistics constructs an empty statistics store.
func NewRequestStatistics() *RequestStatistics {
	return &RequestStatistics{
		apis:           make(map[string]*apiStats),
		requestsByDay:  make(map[string]int64),
		requestsByHour: make(map[int]int64),
		tokensByDay:    make(map[string]int64),
		tokensByHour:   make(map[int]int64),
	}
}

// Record ingests a usage record and updates aggregate counters.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	if !statisticsEnabled.Load() {
		return
	}

	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	detail := normaliseDetail(record.Detail, record.Provider, record.ExecutorType)
	totalTokens := detail.TotalTokens
	statsKey := resolveAPIIdentifier(ctx, record)
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	modelName := strings.TrimSpace(record.Model)
	if modelName == "" {
		modelName = "unknown"
	}
	dayKey := timestamp.Format("2006-01-02")
	hourKey := timestamp.Hour()

	requestDetail := RequestDetail{
		Timestamp:       timestamp,
		LatencyMs:       normaliseLatency(record.Latency),
		TTFTMs:          normaliseLatency(record.TTFT),
		Source:          record.Source,
		AuthIndex:       record.AuthIndex,
		Tokens:          detail,
		Failed:          failed,
		Generate:        coreusage.GenerateEnabled(record.Generate),
		Fail:            FailureSnapshot{StatusCode: record.Fail.StatusCode, Body: strings.TrimSpace(record.Fail.Body)},
		Provider:        strings.TrimSpace(record.Provider),
		ExecutorType:    strings.TrimSpace(record.ExecutorType),
		Alias:           strings.TrimSpace(record.Alias),
		Endpoint:        strings.TrimSpace(internallogging.GetEndpoint(ctx)),
		ReasoningEffort: strings.TrimSpace(record.ReasoningEffort),
		ServiceTier:     strings.TrimSpace(record.ServiceTier),
		ResponseTier:    strings.TrimSpace(record.ResponseServiceTier),
		TokenBreakdown:  coreusage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, record.ExecutorType).TokenBreakdown,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalRequests++
	if failed {
		s.failureCount++
	} else {
		s.successCount++
	}
	s.totalTokens += totalTokens

	stats, ok := s.apis[statsKey]
	if !ok {
		stats = &apiStats{Models: make(map[string]*modelStats)}
		s.apis[statsKey] = stats
	}
	s.updateAPIStats(stats, modelName, requestDetail)

	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
}

func (s *RequestStatistics) updateAPIStats(stats *apiStats, model string, detail RequestDetail) {
	stats.TotalRequests++
	stats.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue, ok := stats.Models[model]
	if !ok {
		modelStatsValue = &modelStats{}
		stats.Models[model] = modelStatsValue
	}
	modelStatsValue.TotalRequests++
	modelStatsValue.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue.Details = append(modelStatsValue.Details, detail)
}

// Snapshot returns a copy of the aggregated metrics.
func (s *RequestStatistics) Snapshot() StatisticsSnapshot {
	result := StatisticsSnapshot{}
	if s == nil {
		return result
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result.TotalRequests = s.totalRequests
	result.SuccessCount = s.successCount
	result.FailureCount = s.failureCount
	result.TotalTokens = s.totalTokens
	result.TokenUsed = s.totalTokens

	result.APIs = make(map[string]APISnapshot, len(s.apis))
	for apiName, stats := range s.apis {
		apiSnapshot := APISnapshot{
			TotalRequests: stats.TotalRequests,
			TotalTokens:   stats.TotalTokens,
			TokenUsed:     stats.TotalTokens,
			Models:        make(map[string]ModelSnapshot, len(stats.Models)),
		}
		for modelName, modelStatsValue := range stats.Models {
			requestDetails := make([]RequestDetail, len(modelStatsValue.Details))
			copy(requestDetails, modelStatsValue.Details)
			apiSnapshot.Models[modelName] = ModelSnapshot{
				TotalRequests: modelStatsValue.TotalRequests,
				TotalTokens:   modelStatsValue.TotalTokens,
				TokenUsed:     modelStatsValue.TotalTokens,
				Details:       requestDetails,
			}
		}
		result.APIs[apiName] = apiSnapshot
	}

	result.RequestsByDay = make(map[string]int64, len(s.requestsByDay))
	for k, v := range s.requestsByDay {
		result.RequestsByDay[k] = v
	}

	result.RequestsByHour = make(map[string]int64, len(s.requestsByHour))
	for hour, v := range s.requestsByHour {
		result.RequestsByHour[formatHour(hour)] = v
	}

	result.TokensByDay = make(map[string]int64, len(s.tokensByDay))
	for k, v := range s.tokensByDay {
		result.TokensByDay[k] = v
	}

	result.TokensByHour = make(map[string]int64, len(s.tokensByHour))
	for hour, v := range s.tokensByHour {
		result.TokensByHour[formatHour(hour)] = v
	}

	return result
}

// MergeResult reports import de-duplication results.
type MergeResult struct {
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}

// MergeSnapshot merges an exported snapshot into the current store.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
	result := MergeResult{}
	if s == nil {
		return result
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]struct{})
	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for _, detail := range modelStatsValue.Details {
				seen[dedupKey(apiName, modelName, detail)] = struct{}{}
			}
		}
	}

	for apiName, apiSnapshot := range snapshot.APIs {
		apiName = strings.TrimSpace(apiName)
		if apiName == "" {
			continue
		}
		stats, ok := s.apis[apiName]
		if !ok || stats == nil {
			stats = &apiStats{Models: make(map[string]*modelStats)}
			s.apis[apiName] = stats
		} else if stats.Models == nil {
			stats.Models = make(map[string]*modelStats)
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				detail.Tokens = normaliseTokenStats(detail.Tokens)
				if detail.LatencyMs < 0 {
					detail.LatencyMs = 0
				}
				if detail.TTFTMs < 0 {
					detail.TTFTMs = 0
				}
				if detail.Timestamp.IsZero() {
					detail.Timestamp = time.Now()
				}
				key := dedupKey(apiName, modelName, detail)
				if _, exists := seen[key]; exists {
					result.Skipped++
					continue
				}
				seen[key] = struct{}{}
				s.recordImported(apiName, modelName, stats, detail)
				result.Added++
			}
		}
	}

	return result
}

func (s *RequestStatistics) recordImported(apiName, modelName string, stats *apiStats, detail RequestDetail) {
	detail.Tokens = normaliseTokenStats(detail.Tokens)
	totalTokens := detail.Tokens.TotalTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	s.totalRequests++
	if detail.Failed {
		s.failureCount++
	} else {
		s.successCount++
	}
	s.totalTokens += totalTokens

	s.updateAPIStats(stats, modelName, detail)

	dayKey := detail.Timestamp.Format("2006-01-02")
	hourKey := detail.Timestamp.Hour()

	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
}

func dedupKey(apiName, modelName string, detail RequestDetail) string {
	tokens := normaliseTokenStats(detail.Tokens)
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%t|%d|%d|%d|%d|%d|%d|%d",
		apiName,
		modelName,
		detail.Timestamp.UTC().Format(time.RFC3339Nano),
		detail.Source,
		detail.AuthIndex,
		detail.Failed,
		detail.Generate,
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.CacheReadTokens,
		tokens.CacheCreationTokens,
		tokens.TotalTokens,
	)
}

func resolveAPIIdentifier(ctx context.Context, record coreusage.Record) string {
	if value := strings.TrimSpace(record.APIKey); value != "" {
		return value
	}
	if value := strings.TrimSpace(record.Source); value != "" {
		return value
	}
	if value := strings.TrimSpace(record.AuthID); value != "" {
		return "auth:" + value
	}
	if value := strings.TrimSpace(record.Provider); value != "" {
		if endpoint := strings.TrimSpace(internallogging.GetEndpoint(ctx)); endpoint != "" {
			return value + " " + endpoint
		}
		return value
	}
	if endpoint := strings.TrimSpace(internallogging.GetEndpoint(ctx)); endpoint != "" {
		return endpoint
	}
	return "unknown"
}

func resolveSuccess(ctx context.Context) bool {
	status := internallogging.GetResponseStatus(ctx)
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

const httpStatusBadRequest = 400

func normaliseDetail(detail coreusage.Detail, provider, executorType string) TokenStats {
	detail = coreusage.EnsureTokenBreakdownForProvider(detail, provider, executorType)
	return normaliseTokenStats(TokenStats{
		InputTokens:         detail.InputTokens,
		OutputTokens:        detail.OutputTokens,
		ReasoningTokens:     detail.ReasoningTokens,
		CachedTokens:        detail.CachedTokens,
		CacheReadTokens:     detail.CacheReadTokens,
		CacheCreationTokens: detail.CacheCreationTokens,
		TotalTokens:         detail.TotalTokens,
		TokenUsed:           detail.TotalTokens,
	})
}

func normaliseTokenStats(tokens TokenStats) TokenStats {
	if tokens.TotalTokens == 0 && tokens.TokenUsed > 0 {
		tokens.TotalTokens = tokens.TokenUsed
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens
	}
	if tokens.TotalTokens < 0 {
		tokens.TotalTokens = 0
	}
	tokens.TokenUsed = tokens.TotalTokens
	return tokens
}

func normaliseLatency(latency time.Duration) int64 {
	if latency <= 0 {
		return 0
	}
	return latency.Milliseconds()
}

func formatHour(hour int) string {
	if hour < 0 {
		hour = 0
	}
	hour = hour % 24
	return fmt.Sprintf("%02d", hour)
}
