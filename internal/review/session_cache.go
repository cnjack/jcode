package review

// sessionCache holds reused reviewer conversations so the large policy prefix is
// served from the provider's prompt cache across reviews (V3). It is a
// placeholder for V1/V2 and is populated in the V3 step.
type sessionCache struct{}

func newSessionCache() *sessionCache { return &sessionCache{} }
