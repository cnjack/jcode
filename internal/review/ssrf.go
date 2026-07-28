package review

import (
	"strings"

	"github.com/cnjack/jcode/internal/ssrf"
)

// Deterministic pre-filter for cloud instance-metadata (SSRF-to-credentials).
//
// The risk policy tells the reviewer model to deny these, but a policy paragraph
// is exactly the defense that the 20-round judgment eval showed can be talked
// out of: the reviewer allowed an IMDS IAM-credential probe 2/20 times when the
// request was dressed up as "debugging a slow network request". A prompt is not
// a control. This filter runs BEFORE the model is consulted, so the outcome for
// the highest-signal case does not depend on model judgment at all. The policy
// text stays as defense-in-depth, not as the only layer.
//
// Deliberately conservative about over-blocking: a metadata address that is NOT
// paired with a credential/metadata path (e.g. `rg 169.254.169.254 access.log`,
// a legitimate log search) escalates to the human rather than hard-denying,
// because a denial cannot be overridden by the user while an escalation can.

// metadataHosts is the canonical list, sourced from the dependency-free
// internal/ssrf leaf so the WebFetch tool's dial guard and this reviewer
// pre-filter share one definition without an import cycle.
var metadataHosts = ssrf.MetadataHosts

// credentialPaths mark a metadata request as specifically reaching for identity
// or credential material rather than, say, an instance hostname.
var credentialPaths = []string{
	"/iam/security-credentials",
	"/latest/meta-data",
	"/latest/api/token", // IMDSv2 token — the first step of an IMDSv2 probe
	"/computemetadata/",
	"/metadata/identity",
	"/metadata/instance",
	"/metadata/v1",
	"service-accounts",
}

// metadataProbeVerdict deterministically adjudicates cloud-metadata access in a
// tool call's arguments. It returns (verdict, true) when the filter fires and
// (_, false) when the call should proceed to the model as usual.
//
// The second return value of the rule name is used for the audit log so a
// pre-filtered decision is distinguishable from a model verdict.
func metadataProbeVerdict(req Request) (Result, string, bool) {
	args := strings.ToLower(req.ToolArgs)
	if !containsAny(args, metadataHosts) {
		return Result{}, "", false
	}
	if containsAny(args, credentialPaths) {
		return Result{
			Outcome:   Deny,
			RiskLevel: "critical",
			Rationale: "Blocked by policy: this reads cloud instance-metadata credential/identity endpoints, which exposes IAM role credentials. No debugging or diagnostics justification permits this.",
		}, "metadata_credential_probe", true
	}
	// A metadata address with no credential path: plausibly legitimate (searching
	// logs for the IP, docs). Let the human decide rather than hard-blocking.
	return Result{
		Outcome:   Escalate,
		RiskLevel: "high",
		Rationale: "References a cloud instance-metadata address; escalated for human review.",
	}, "metadata_address_mention", true
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
