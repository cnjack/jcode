// Package toolstate contains transport-neutral long-running tool presentation
// state. It is intentionally dependency-free so tools and handlers can share
// it without an import cycle.
package toolstate

type Surface string

const (
	SurfaceActivity   Surface = "activity"
	SurfaceStandalone Surface = "standalone"
)

type Phase string

const (
	PhaseQueued     Phase = "queued"
	PhaseGenerating Phase = "generating"
	PhaseSaving     Phase = "saving"
	PhaseSucceeded  Phase = "succeeded"
	PhaseFailed     Phase = "failed"
	PhaseCancelled  Phase = "cancelled"
	PhaseUncertain  Phase = "uncertain"
)

type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeUncertain Outcome = "uncertain"
)

type ArtifactRef struct {
	ID          string `json:"id"`
	Storage     string `json:"storage"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	MediaType   string `json:"media_type"`
	Size        int64  `json:"size"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	Shareable   bool   `json:"shareable,omitempty"`
}

type ProgressEvent struct {
	Name        string
	ToolCallID  string
	Surface     Surface
	Phase       Phase
	OperationID string
	ErrorCode   string
	Provider    string
	Model       string
	Artifacts   []ArtifactRef
}
