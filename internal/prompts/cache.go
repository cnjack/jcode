package prompts

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// PromptBlockCache caches static prompt blocks to avoid redundant recomputation.
type PromptBlockCache struct {
	mu          sync.RWMutex
	staticHash  string
	staticBlock *PromptBlock
}

// NewPromptBlockCache returns a new empty cache.
func NewPromptBlockCache() *PromptBlockCache {
	return &PromptBlockCache{}
}

// GetOrBuild returns a cached block if the content hash matches, otherwise calls
// buildFn to create a new block and caches it.
func (c *PromptBlockCache) GetOrBuild(contentHash string, buildFn func() *PromptBlock) *PromptBlock {
	c.mu.RLock()
	if c.staticHash == contentHash && c.staticBlock != nil {
		block := c.staticBlock
		c.mu.RUnlock()
		return block
	}
	c.mu.RUnlock()

	block := buildFn()

	c.mu.Lock()
	c.staticHash = contentHash
	c.staticBlock = block
	c.mu.Unlock()

	return block
}

// Invalidate clears the cached block.
func (c *PromptBlockCache) Invalidate() {
	c.mu.Lock()
	c.staticHash = ""
	c.staticBlock = nil
	c.mu.Unlock()
}

// ContentHash returns the first 16 hex characters of the SHA-256 digest of content.
func ContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8])
}
