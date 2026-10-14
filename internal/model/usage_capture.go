package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// go-openai's Usage schema only knows prompt_tokens_details.cached_tokens, so
// provider-specific cache counters (cache writes in particular) are dropped
// during decoding. usageCapture side-channels them: headerDoer tees the raw
// response body through a usageSniffer that parses only the usage object,
// and recordUsage merges what it found into AddParams.

// Byte caps for the side-channel parser. They bound only the sniffer's own
// buffering; go-openai still reads the full body.
const (
	maxUsageSniffJSONBytes = 4 << 20
	maxUsageSniffLineBytes = 1 << 20
)

type usageCaptureKey struct{}

// usageCapture holds the provider-specific cache counters seen on one API
// call's response.
type usageCapture struct {
	mu         sync.Mutex
	cacheWrite int
	cacheRead  int
	seen       bool
}

// withUsageCapture attaches a fresh usageCapture to ctx so headerDoer can
// populate it from the raw response of the request issued with that ctx.
func withUsageCapture(ctx context.Context) context.Context {
	return context.WithValue(ctx, usageCaptureKey{}, &usageCapture{})
}

func usageCaptureFromContext(ctx context.Context) *usageCapture {
	c, _ := ctx.Value(usageCaptureKey{}).(*usageCapture)
	return c
}

func (c *usageCapture) set(cacheWrite, cacheRead int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Streams repeat cumulative usage; the last report wins.
	c.cacheWrite = cacheWrite
	c.cacheRead = cacheRead
	c.seen = true
}

// apply merges captured counters into p. Standard go-openai fields win; the
// captured cache-read only fills in when the provider sent no
// prompt_tokens_details at all.
func (c *usageCapture) apply(p *AddParams) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.seen {
		return
	}
	if p.CacheWrite == 0 && c.cacheWrite > 0 {
		p.CacheWrite = c.cacheWrite
	}
	if !p.CacheDetailsPresent && p.Cached == 0 && c.cacheRead > 0 {
		p.Cached = c.cacheRead
		p.CacheDetailsPresent = true
	}
}

// providerUsageWire lists the cache counters different OpenAI-compatible
// providers put next to the standard usage fields:
//   - OpenRouter: prompt_tokens_details.cache_write_tokens
//   - LiteLLM: prompt_tokens_details.cache_creation_tokens and top-level
//     cache_creation_input_tokens / cache_read_input_tokens
//   - Anthropic-style gateways: cache_creation_input_tokens / cache_read_input_tokens
//   - DeepSeek: prompt_cache_hit_tokens
//   - Moonshot/Kimi: top-level cached_tokens
type providerUsageWire struct {
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	PromptCacheHitTokens     int `json:"prompt_cache_hit_tokens"`
	CachedTokens             int `json:"cached_tokens"`
	PromptTokensDetails      *struct {
		CacheWriteTokens         int `json:"cache_write_tokens"`
		CacheCreationTokens      int `json:"cache_creation_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u *providerUsageWire) cacheWrite() int {
	if d := u.PromptTokensDetails; d != nil {
		if v := firstPositive(d.CacheWriteTokens, d.CacheCreationTokens, d.CacheCreationInputTokens); v > 0 {
			return v
		}
	}
	return firstPositive(u.CacheCreationInputTokens)
}

func (u *providerUsageWire) cacheRead() int {
	return firstPositive(u.CacheReadInputTokens, u.PromptCacheHitTokens, u.CachedTokens)
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// parseProviderUsage extracts provider cache counters from one JSON payload
// (a whole response or a single SSE data line). ok is false when the payload
// carries no usage object.
func parseProviderUsage(payload []byte) (cacheWrite, cacheRead int, ok bool) {
	if !bytes.Contains(payload, []byte(`"usage"`)) || !bytes.Contains(payload, []byte("cache")) {
		return 0, 0, false
	}
	var wire struct {
		Usage *providerUsageWire `json:"usage"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil || wire.Usage == nil {
		return 0, 0, false
	}
	return wire.Usage.cacheWrite(), wire.Usage.cacheRead(), true
}

// usageSniffer wraps a response body and parses usage from the bytes as the
// caller reads them. SSE bodies are parsed line by line; other bodies are
// buffered (up to maxUsageSniffJSONBytes) and parsed once on EOF/Close.
type usageSniffer struct {
	body    io.ReadCloser
	capture *usageCapture
	sse     bool

	buf      []byte
	overflow bool // the current line / body exceeded its cap; skip it
	done     bool
}

func newUsageSniffer(resp *http.Response, capture *usageCapture) io.ReadCloser {
	return &usageSniffer{
		body:    resp.Body,
		capture: capture,
		sse:     strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream"),
	}
}

func (s *usageSniffer) Read(p []byte) (int, error) {
	n, err := s.body.Read(p)
	if n > 0 && !s.done {
		s.feed(p[:n])
	}
	if err == io.EOF {
		s.finish()
	}
	return n, err
}

func (s *usageSniffer) Close() error {
	s.finish()
	return s.body.Close()
}

func (s *usageSniffer) feed(chunk []byte) {
	if !s.sse {
		if s.overflow {
			return
		}
		if len(s.buf)+len(chunk) > maxUsageSniffJSONBytes {
			s.overflow = true
			s.buf = nil
			return
		}
		s.buf = append(s.buf, chunk...)
		return
	}
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			s.appendLine(chunk)
			return
		}
		s.appendLine(chunk[:i])
		s.flushLine()
		chunk = chunk[i+1:]
	}
}

func (s *usageSniffer) appendLine(part []byte) {
	if s.overflow {
		return
	}
	if len(s.buf)+len(part) > maxUsageSniffLineBytes {
		s.overflow = true
		s.buf = s.buf[:0]
		return
	}
	s.buf = append(s.buf, part...)
}

func (s *usageSniffer) flushLine() {
	line := bytes.TrimSpace(s.buf)
	if !s.overflow {
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			s.parse(bytes.TrimSpace(data))
		}
	}
	s.buf = s.buf[:0]
	s.overflow = false
}

func (s *usageSniffer) finish() {
	if s.done {
		return
	}
	s.done = true
	if s.sse {
		s.flushLine()
		return
	}
	if !s.overflow {
		s.parse(s.buf)
	}
	s.buf = nil
}

func (s *usageSniffer) parse(payload []byte) {
	if cw, cr, ok := parseProviderUsage(payload); ok {
		s.capture.set(cw, cr)
	}
}
