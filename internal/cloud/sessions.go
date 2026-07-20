// sessions.go mirrors the local session index (~/.jcode/sessions/session.json)
// to the cloud: one full upsert at startup, then an upsert whenever the index
// file changes (detected by mtime polling — deliberately no fsnotify). The
// upsert response carries each session's last_seq, which seeds the event
// pump's per-session seq allocator (续号).
package cloud

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
)

// collectSessions builds the upsert list from the local session index.
// Status is "running" only for sessions the local web layer marked running;
// everything else reports "idle". Meta is the SessionMeta JSON, as-is.
func (c *Connector) collectSessions() ([]SessionUpsert, error) {
	listFn := c.cfg.ListSessionsFn
	if listFn == nil {
		listFn = session.ListAllSessions
	}
	all, err := listFn()
	if err != nil {
		return nil, err
	}
	upserts := make([]SessionUpsert, 0, len(all))
	for _, metas := range all {
		for _, m := range metas {
			if m.UUID == "" {
				continue
			}
			status := "idle"
			if m.Status == "running" {
				status = "running"
			}
			metaJSON, err := json.Marshal(m)
			if err != nil {
				continue
			}
			upserts = append(upserts, SessionUpsert{
				SessionID: m.UUID,
				Status:    status,
				Meta:      metaJSON,
			})
		}
	}
	return upserts, nil
}

// syncSessions performs one upsert round and seeds the seq allocator from the
// server's last_seq answers.
func (c *Connector) syncSessions(ctx context.Context) error {
	upserts, err := c.collectSessions()
	if err != nil {
		return err
	}
	resp, err := c.client.UpsertSessions(ctx, c.token, upserts)
	if err != nil {
		return err
	}
	for _, s := range resp.Sessions {
		c.seq.Seed(s.SessionID, s.LastSeq)
	}
	c.logf("session index synced (%d sessions)", len(upserts))
	return nil
}

// sessionSyncLoop re-upserts whenever the session index file's mtime changes.
func (c *Connector) sessionSyncLoop(ctx context.Context) {
	pathFn := c.cfg.IndexPathFn
	if pathFn == nil {
		pathFn = config.SessionsIndexPath
	}
	path, err := pathFn()
	if err != nil {
		c.logf("session index path unavailable, index sync disabled: %v", err)
		return
	}

	// Baseline: Run already did the initial full upsert, so only react to
	// changes after this point.
	lastMod := time.Time{}
	if fi, err := os.Stat(path); err == nil {
		lastMod = fi.ModTime()
	}

	ticker := time.NewTicker(c.indexPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fi, err := os.Stat(path)
			if err != nil {
				continue // index may not exist yet
			}
			if fi.ModTime().Equal(lastMod) {
				continue
			}
			lastMod = fi.ModTime()
			if err := c.syncSessions(ctx); err != nil && ctx.Err() == nil {
				c.logf("session upsert failed: %v", err)
			}
		}
	}
}
