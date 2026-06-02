package tui

import (
	"time"

	"github.com/cnjack/jcode/internal/config"
)

const (
	tuiPerfLogInterval     = 30 * time.Second
	tuiSlowContentRender   = 50 * time.Millisecond
	tuiSlowViewRender      = 80 * time.Millisecond
	tuiSlowImmediateLogGap = 5 * time.Second
	tuiPerfImmediateReason = "slow"
	tuiPerfPeriodicReason  = "periodic"
	tuiPerfStartReason     = "start"
)

type tuiRenderPerf struct {
	startedAt time.Time
	lastLog   time.Time
	lastSlow  time.Time

	viewCalls          uint64
	contentRenderCalls uint64
	contentCacheHits   uint64
	contentCacheMisses uint64
	sidebarCacheHits   uint64
	sidebarCacheMisses uint64
	footerCacheHits    uint64
	footerCacheMisses  uint64
	refreshes          uint64
	agentTextMsgs      uint64
	batchRenderMsgs    uint64
	slowContentRenders uint64
	slowViews          uint64

	maxContentRender time.Duration
	maxViewRender    time.Duration
	lastContentBytes int
	lastLineCount    int
	lastContentWidth int
}

func newTUIRenderPerf() tuiRenderPerf {
	now := time.Now()
	return tuiRenderPerf{startedAt: now, lastLog: now}
}

func (m *Model) ensureRenderPerf() {
	if m.renderPerf.startedAt.IsZero() {
		m.renderPerf = newTUIRenderPerf()
	}
}

func (m *Model) observeViewRender(elapsed time.Duration) {
	m.ensureRenderPerf()
	if elapsed > m.renderPerf.maxViewRender {
		m.renderPerf.maxViewRender = elapsed
	}
	if elapsed >= tuiSlowViewRender {
		m.renderPerf.slowViews++
		m.logRenderPerf(tuiPerfImmediateReason)
		return
	}
	m.logRenderPerf(tuiPerfPeriodicReason)
}

func (m *Model) observeContentRender(elapsed time.Duration, contentBytes int) {
	m.ensureRenderPerf()
	if elapsed > m.renderPerf.maxContentRender {
		m.renderPerf.maxContentRender = elapsed
	}
	if elapsed >= tuiSlowContentRender {
		m.renderPerf.slowContentRenders++
		m.logRenderPerf(tuiPerfImmediateReason)
	}
	m.renderPerf.lastContentBytes = contentBytes
	m.renderPerf.lastLineCount = len(m.lines)
	m.renderPerf.lastContentWidth = m.contentWidth()
}

func (m *Model) logRenderPerf(reason string) {
	m.ensureRenderPerf()
	now := time.Now()
	switch reason {
	case tuiPerfStartReason:
	case tuiPerfImmediateReason:
		if now.Sub(m.renderPerf.lastSlow) < tuiSlowImmediateLogGap {
			return
		}
		m.renderPerf.lastSlow = now
	default:
		if now.Sub(m.renderPerf.lastLog) < tuiPerfLogInterval {
			return
		}
		reason = tuiPerfPeriodicReason
	}
	m.renderPerf.lastLog = now

	totalContent := m.renderPerf.contentCacheHits + m.renderPerf.contentCacheMisses
	contentHitRate := 0.0
	if totalContent > 0 {
		contentHitRate = float64(m.renderPerf.contentCacheHits) / float64(totalContent) * 100
	}

	totalSidebar := m.renderPerf.sidebarCacheHits + m.renderPerf.sidebarCacheMisses
	sidebarHitRate := 0.0
	if totalSidebar > 0 {
		sidebarHitRate = float64(m.renderPerf.sidebarCacheHits) / float64(totalSidebar) * 100
	}

	totalFooter := m.renderPerf.footerCacheHits + m.renderPerf.footerCacheMisses
	footerHitRate := 0.0
	if totalFooter > 0 {
		footerHitRate = float64(m.renderPerf.footerCacheHits) / float64(totalFooter) * 100
	}

	config.Logger().Printf(
		"[tui.perf] reason=%s uptime=%s view_calls=%d content_calls=%d content_hit_rate=%.1f%% sidebar_hit_rate=%.1f%% footer_hit_rate=%.1f%% refreshes=%d stream_msgs=%d batch_renders=%d slow_content=%d slow_views=%d max_content=%s max_view=%s lines=%d bytes=%d width=%d",
		reason,
		now.Sub(m.renderPerf.startedAt).Truncate(time.Second),
		m.renderPerf.viewCalls,
		m.renderPerf.contentRenderCalls,
		contentHitRate,
		sidebarHitRate,
		footerHitRate,
		m.renderPerf.refreshes,
		m.renderPerf.agentTextMsgs,
		m.renderPerf.batchRenderMsgs,
		m.renderPerf.slowContentRenders,
		m.renderPerf.slowViews,
		m.renderPerf.maxContentRender,
		m.renderPerf.maxViewRender,
		m.renderPerf.lastLineCount,
		m.renderPerf.lastContentBytes,
		m.renderPerf.lastContentWidth,
	)
}
