package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// usageTabModel displays accumulated usage statistics.
type usageTabModel struct {
	client   *Client
	viewport viewport.Model
	usage    map[string]any
	err      error
	width    int
	height   int
	ready    bool
}

type usageDataMsg struct {
	usage map[string]any
	err   error
}

func newUsageTabModel(client *Client) usageTabModel {
	return usageTabModel{client: client}
}

func (m usageTabModel) Init() tea.Cmd {
	return m.fetchData
}

func (m usageTabModel) fetchData() tea.Msg {
	usage, err := m.client.GetUsage()
	return usageDataMsg{usage: usage, err: err}
}

func (m usageTabModel) Update(msg tea.Msg) (usageTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case localeChangedMsg:
		m.viewport.SetContent(m.renderContent())
		return m, nil
	case usageDataMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.usage = msg.usage
		}
		m.viewport.SetContent(m.renderContent())
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "r" {
			return m, m.fetchData
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *usageTabModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.ready {
		m.viewport = viewport.New(w, h)
		m.viewport.SetContent(m.renderContent())
		m.ready = true
		return
	}
	m.viewport.Width = w
	m.viewport.Height = h
}

func (m usageTabModel) View() string {
	if !m.ready {
		return T("loading")
	}
	return m.viewport.View()
}

func (m usageTabModel) renderContent() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render(T("usage_title")))
	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render(T("usage_help")))
	sb.WriteString("\n\n")

	if m.err != nil {
		sb.WriteString(errorStyle.Render("Error: " + m.err.Error()))
		sb.WriteString("\n")
		return sb.String()
	}

	usageMap, _ := m.usage["usage"].(map[string]any)
	if usageMap == nil {
		sb.WriteString(subtitleStyle.Render(T("usage_no_data")))
		sb.WriteString("\n")
		return sb.String()
	}

	totalReqs := int64(getFloat(usageMap, "total_requests"))
	successCnt := int64(getFloat(usageMap, "success_count"))
	failureCnt := int64(getFloat(usageMap, "failure_count"))
	totalTokens := usageTokenTotal(usageMap)

	cardWidth := 20
	if m.width > 0 {
		cardWidth = (m.width - 6) / 4
		if cardWidth < 16 {
			cardWidth = 16
		}
	}
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(cardWidth).
		Height(3)

	rpm := ratePerMinute(totalReqs, usageMap, "requests_by_hour")
	tpm := ratePerMinute(totalTokens, usageMap, "tokens_by_hour")
	cards := []string{
		cardStyle.Copy().BorderForeground(colorInfo).Render(fmt.Sprintf(
			"%s\n%s\n%s",
			T("usage_total_reqs"),
			lipgloss.NewStyle().Bold(true).Foreground(colorInfo).Render(fmt.Sprintf("%d", totalReqs)),
			fmt.Sprintf("%s:%d %s:%d", T("usage_success"), successCnt, T("usage_failure"), failureCnt),
		)),
		cardStyle.Copy().BorderForeground(colorWarning).Render(fmt.Sprintf(
			"%s\n%s\n%s",
			T("usage_total_tokens"),
			lipgloss.NewStyle().Bold(true).Foreground(colorWarning).Render(formatLargeNumber(totalTokens)),
			fmt.Sprintf("token_used: %s", formatLargeNumber(totalTokens)),
		)),
		cardStyle.Copy().BorderForeground(colorSuccess).Render(fmt.Sprintf(
			"%s\n%s\n%s",
			T("usage_rpm"),
			lipgloss.NewStyle().Bold(true).Foreground(colorSuccess).Render(fmt.Sprintf("%.2f", rpm)),
			fmt.Sprintf("%s: %d", T("usage_total_reqs"), totalReqs),
		)),
		cardStyle.Copy().BorderForeground(colorHighlight).Render(fmt.Sprintf(
			"%s\n%s\n%s",
			T("usage_tpm"),
			lipgloss.NewStyle().Bold(true).Foreground(colorHighlight).Render(fmt.Sprintf("%.2f", tpm)),
			fmt.Sprintf("%s: %s", T("usage_total_tokens"), formatLargeNumber(totalTokens)),
		)),
	}
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cards[0], " ", cards[1], " ", cards[2], " ", cards[3]))
	sb.WriteString("\n\n")

	m.renderChartSection(&sb, usageMap, "requests_by_hour", T("usage_req_by_hour"), colorInfo)
	m.renderChartSection(&sb, usageMap, "tokens_by_hour", T("usage_tok_by_hour"), colorWarning)
	m.renderChartSection(&sb, usageMap, "requests_by_day", T("usage_req_by_day"), colorSuccess)
	m.renderAPIDetails(&sb, usageMap)

	return sb.String()
}

func usageTokenTotal(usageMap map[string]any) int64 {
	total := int64(getFloat(usageMap, "token_used"))
	if total == 0 {
		total = int64(getFloat(usageMap, "total_tokens"))
	}
	return total
}

func ratePerMinute(total int64, usageMap map[string]any, bucketKey string) float64 {
	if total <= 0 {
		return 0
	}
	buckets, ok := usageMap[bucketKey].(map[string]any)
	if !ok || len(buckets) == 0 {
		return 0
	}
	return float64(total) / float64(len(buckets)) / 60.0
}

func (m usageTabModel) renderChartSection(sb *strings.Builder, usageMap map[string]any, key, title string, color lipgloss.Color) {
	data, ok := usageMap[key].(map[string]any)
	if !ok || len(data) == 0 {
		return
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorHighlight).Render(title))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", minInt(m.width, 60)))
	sb.WriteString("\n")
	sb.WriteString(renderBarChart(data, m.width-6, color))
	sb.WriteString("\n")
}

func (m usageTabModel) renderAPIDetails(sb *strings.Builder, usageMap map[string]any) {
	apis, ok := usageMap["apis"].(map[string]any)
	if !ok || len(apis) == 0 {
		return
	}

	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorHighlight).Render(T("usage_api_detail")))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", minInt(m.width, 80)))
	sb.WriteString("\n")

	header := fmt.Sprintf("  %-30s %10s %12s", "API", T("requests"), T("tokens"))
	sb.WriteString(tableHeaderStyle.Render(header))
	sb.WriteString("\n")

	keys := make([]string, 0, len(apis))
	for apiName := range apis {
		keys = append(keys, apiName)
	}
	sort.Strings(keys)

	for _, apiName := range keys {
		apiMap, ok := apis[apiName].(map[string]any)
		if !ok {
			continue
		}
		apiReqs := int64(getFloat(apiMap, "total_requests"))
		apiTokens := usageTokenTotal(apiMap)
		row := fmt.Sprintf("  %-30s %10d %12s", truncate(maskKey(apiName), 30), apiReqs, formatLargeNumber(apiTokens))
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(row))
		sb.WriteString("\n")

		models, ok := apiMap["models"].(map[string]any)
		if !ok {
			continue
		}
		modelNames := make([]string, 0, len(models))
		for model := range models {
			modelNames = append(modelNames, model)
		}
		sort.Strings(modelNames)
		for _, model := range modelNames {
			stats, ok := models[model].(map[string]any)
			if !ok {
				continue
			}
			modelReqs := int64(getFloat(stats, "total_requests"))
			modelTokens := usageTokenTotal(stats)
			modelRow := fmt.Sprintf("    - %-28s %10d %12s", truncate(model, 28), modelReqs, formatLargeNumber(modelTokens))
			sb.WriteString(tableCellStyle.Render(modelRow))
			sb.WriteString("\n")
			sb.WriteString(m.renderTokenBreakdown(stats))
			sb.WriteString(m.renderLatencyBreakdown(stats))
		}
	}
}

func (m usageTabModel) renderTokenBreakdown(modelStats map[string]any) string {
	details, ok := modelStats["details"].([]any)
	if !ok || len(details) == 0 {
		return ""
	}

	var inputTotal, outputTotal, cachedTotal, reasoningTotal int64
	for _, d := range details {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		tokens, ok := dm["tokens"].(map[string]any)
		if !ok {
			continue
		}
		inputTotal += int64(getFloat(tokens, "input_tokens"))
		outputTotal += int64(getFloat(tokens, "output_tokens"))
		cachedTotal += int64(getFloat(tokens, "cached_tokens"))
		reasoningTotal += int64(getFloat(tokens, "reasoning_tokens"))
	}

	parts := make([]string, 0, 4)
	if inputTotal > 0 {
		parts = append(parts, fmt.Sprintf("%s:%s", T("usage_input"), formatLargeNumber(inputTotal)))
	}
	if outputTotal > 0 {
		parts = append(parts, fmt.Sprintf("%s:%s", T("usage_output"), formatLargeNumber(outputTotal)))
	}
	if cachedTotal > 0 {
		parts = append(parts, fmt.Sprintf("%s:%s", T("usage_cached"), formatLargeNumber(cachedTotal)))
	}
	if reasoningTotal > 0 {
		parts = append(parts, fmt.Sprintf("%s:%s", T("usage_reasoning"), formatLargeNumber(reasoningTotal)))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("      %s\n", lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Join(parts, "  ")))
}

func (m usageTabModel) renderLatencyBreakdown(modelStats map[string]any) string {
	details, ok := modelStats["details"].([]any)
	if !ok || len(details) == 0 {
		return ""
	}

	var totalLatency int64
	var count int
	var minLatency, maxLatency int64
	for _, d := range details {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		latencyMs := int64(getFloat(dm, "latency_ms"))
		if latencyMs <= 0 {
			continue
		}
		totalLatency += latencyMs
		count++
		if count == 1 || latencyMs < minLatency {
			minLatency = latencyMs
		}
		if latencyMs > maxLatency {
			maxLatency = latencyMs
		}
	}
	if count == 0 {
		return ""
	}

	avgLatency := totalLatency / int64(count)
	return fmt.Sprintf("      %s: avg %dms  min %dms  max %dms\n",
		lipgloss.NewStyle().Foreground(colorMuted).Render(T("usage_time")),
		avgLatency,
		minLatency,
		maxLatency,
	)
}

func renderBarChart(data map[string]any, maxBarWidth int, barColor lipgloss.Color) string {
	if maxBarWidth < 10 {
		maxBarWidth = 10
	}

	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var maxValue float64
	for _, key := range keys {
		value := getFloat(data, key)
		if value > maxValue {
			maxValue = value
		}
	}
	if maxValue <= 0 {
		return ""
	}

	labelWidth := 12
	barAvail := maxBarWidth - labelWidth - 12
	if barAvail < 5 {
		barAvail = 5
	}

	barStyle := lipgloss.NewStyle().Foreground(barColor)
	var sb strings.Builder
	for _, key := range keys {
		value := getFloat(data, key)
		barLen := int(value / maxValue * float64(barAvail))
		if barLen < 1 && value > 0 {
			barLen = 1
		}
		label := truncate(key, labelWidth)
		sb.WriteString(fmt.Sprintf("  %-*s %s %s\n",
			labelWidth,
			label,
			barStyle.Render(strings.Repeat("#", barLen)),
			lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("%.0f", value)),
		))
	}
	return sb.String()
}
