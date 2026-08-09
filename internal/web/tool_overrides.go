package web

const (
	SessionToolDisabledNoModel               = "no_model"
	SessionToolDisabledProviderDisabled      = "provider_disabled"
	SessionToolDisabledUnsupported           = "unsupported"
	SessionToolDisabledConnectionUnavailable = "connection_unavailable"
	SessionToolDisabledPlanMode              = "plan_mode"
)

// SessionToolEvaluation is retained as a transport-neutral capability result
// used by command-side availability tests. It is no longer persisted or
// exposed as a per-session product setting.
type SessionToolEvaluation struct {
	Available      bool
	Effective      bool
	DisabledReason string
}
