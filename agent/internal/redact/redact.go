// internal/redact/redact.go
// Redacts secrets and credentials from strings before they are sent to an LLM.
//
// SECURITY CONTRACT
// -----------------
// No raw log lines, environment variable values, or HTTP headers that could
// contain tokens or passwords may be included in an LLM prompt without first
// passing through Redact(). This package is the single enforcement point.
//
// Redaction strategy:
//   1. Bearer tokens in Authorization headers.
//   2. Basic auth credentials in HTTP Authorization headers.
//   3. Passwords embedded in connection URIs (mongodb://, postgresql://, etc.).
//   4. Key=value and key:value patterns where the key name suggests a secret.
//   5. Long hex/base64 strings that look like API keys or JWTs.
//
// The replacement value is the literal string [REDACTED] so the LLM can see
// that a value existed but cannot learn its content.

package redact

import (
	"regexp"
	"strings"
)

const redacted = "[REDACTED]"

// compiled once at package init — regexp compilation is expensive.
var patterns = []*regexp.Regexp{
	// Bearer token: "Bearer <token>"
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/]+=*`),

	// Basic auth header value: "Basic <base64>"
	regexp.MustCompile(`(?i)(basic\s+)[A-Za-z0-9+/]+=*`),

	// Password in URI: scheme://user:PASSWORD@host
	regexp.MustCompile(`(?i)(://[^:@/\s]+:)[^@\s]+(@)`),

	// Key=value or key:value where key implies a secret.
	// Matches: password=foo, api_key=foo, secret=foo, token=foo, auth=foo,
	//          apikey: foo, api-key: foo, authorization: foo  (case-insensitive)
	regexp.MustCompile(`(?i)((?:password|passwd|api[_\-]?key|secret|token|auth(?:orization)?|credential|private[_\-]?key)\s*[=:]\s*)(\S+)`),

	// JWT: three base64url segments separated by dots (header.payload.signature)
	regexp.MustCompile(`[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),

	// Long hex strings (≥32 chars) that look like API keys or hashes.
	regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`),
}

// replacements maps each pattern to a replacement template.
// Patterns that use capture groups use $1[REDACTED] or $1[REDACTED]$2.
// Patterns without capture groups replace the entire match.
var replacements = []string{
	"$1" + redacted,       // Bearer token
	"$1" + redacted,       // Basic auth
	"$1" + redacted + "$3", // URI password — needs 3 groups, fix below
	"$1" + redacted,       // key=value
	redacted,              // JWT
	redacted,              // hex string
}

// Redact replaces all recognized secret patterns in s with [REDACTED].
// It is safe to call on multi-line strings.
func Redact(s string) string {
	// URI password pattern has three groups: (scheme://user:)(password)(@host)
	// Rebuild replacement for that specific pattern.
	result := s

	// Pattern 0: Bearer token — replace whole match keeping "Bearer "
	result = patterns[0].ReplaceAllString(result, "${1}"+redacted)

	// Pattern 1: Basic auth — replace whole match keeping "Basic "
	result = patterns[1].ReplaceAllString(result, "${1}"+redacted)

	// Pattern 2: URI password — keep user and @ but redact password
	result = patterns[2].ReplaceAllStringFunc(result, func(m string) string {
		return patterns[2].ReplaceAllString(m, "${1}"+redacted+"${2}")
	})

	// Pattern 3: key=value
	result = patterns[3].ReplaceAllString(result, "${1}"+redacted)

	// Pattern 4: JWT
	result = patterns[4].ReplaceAllString(result, redacted)

	// Pattern 5: long hex strings
	result = patterns[5].ReplaceAllString(result, redacted)

	return result
}

// RedactLines applies Redact to each line of a multi-line string and
// reassembles the result. This is the function to use for log excerpts.
func RedactLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = Redact(line)
	}
	return strings.Join(lines, "\n")
}
