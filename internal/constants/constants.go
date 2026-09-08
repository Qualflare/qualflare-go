// Package constants holds the caps the Qualflare wire contract and server enforce.
//
// Ported verbatim from the sibling reporters' shared/constants.ts and
// qualflare-pytest's constants.py, which are in turn derived from api-service's
// launch.go. Keep the numbers identical across packages: a report that is valid
// from one reporter must be valid from all of them.
package constants

const (
	MaxSuitesPerLaunch = 2000
	MaxCasesPerSuite   = 5000

	// The server's hard cap is 1000 steps per case. The client stops at 300 per
	// attempt and warns, so a runaway suite is bounded before it reaches the
	// body limit rather than being silently truncated on write.
	MaxStepsPerCase        = 1000
	MaxStepsPerTestAttempt = 300
	MaxParametersPerStep   = 50

	MaxAttachmentsPerCase = 50
	MaxLabelsPerCase      = 100
	MaxLinksPerCase       = 20
	MaxTagsPerCase        = 64
	MaxTagLength          = 255

	MaxAttemptsPerCase     = 50
	MaxAttemptMessageRunes = 8192
	MaxAttemptTraceRunes   = 32768

	// Truncated server-side rather than rejected, so the full text is sent and
	// the server decides. Kept here only to size the client's own guard rails.
	MaxCaseErrorRunes = 65536

	MaxAttachmentInlineChars = 2_097_152
)
