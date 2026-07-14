package review

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
)

// reviewWithTools is the V2 read-only investigation path: the reviewer may run
// read/grep/glob to gather evidence before deciding. It is implemented in the V2
// step; until then it defers to the single-shot path so the dispatcher compiles.
func (e *Engine) reviewWithTools(ctx context.Context, req Request, cm einomodel.ToolCallingChatModel) (Result, reviewMeta) {
	return e.reviewSingleShot(ctx, req, cm)
}
