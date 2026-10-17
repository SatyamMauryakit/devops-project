package redact_test

import (
	"strings"
	"testing"

	"github.com/devops-sentinel/agent/internal/redact"
)

func TestRedact_BearerToken(t *testing.T) {
	input := `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig`
	got := redact.Redact(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("bearer token not redacted: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output: %q", got)
	}
}

func TestRedact_BasicAuth(t *testing.T) {
	input := `Authorization: Basic dXNlcjpwYXNzd29yZA==`
	got := redact.Redact(input)
	if strings.Contains(got, "dXNlcjpwYXNzd29yZA==") {
		t.Errorf("basic auth not redacted: %q", got)
	}
}

func TestRedact_URIPassword(t *testing.T) {
	cases := []struct {
		input    string
		mustKeep string // part that must survive
	}{
		{
			input:    `mongodb://admin:supersecret@mongo:27017/sentinel`,
			mustKeep: "mongo:27017",
		},
		{
			input:    `postgresql://user:p4ssw0rd@db:5432/temporal`,
			mustKeep: "db:5432",
		},
	}

	for _, tc := range cases {
		got := redact.Redact(tc.input)
		if strings.Contains(got, "supersecret") || strings.Contains(got, "p4ssw0rd") {
			t.Errorf("URI password not redacted in %q, got: %q", tc.input, got)
		}
		if !strings.Contains(got, tc.mustKeep) {
			t.Errorf("expected host %q to survive redaction, got: %q", tc.mustKeep, got)
		}
	}
}

func TestRedact_KeyValuePatterns(t *testing.T) {
	cases := []struct {
		input    string
		badValue string
	}{
		{`password=hunter2`, "hunter2"},
		{`api_key=sk-abc123def456`, "sk-abc123def456"},
		{`secret: my-top-secret`, "my-top-secret"},
		{`WHATSAPP_ACCESS_TOKEN=EAABvery_long_token`, "EAABvery_long_token"},
		{`authorization: mysecretvalue`, "mysecretvalue"},
	}

	for _, tc := range cases {
		got := redact.Redact(tc.input)
		if strings.Contains(got, tc.badValue) {
			t.Errorf("value %q not redacted in %q, got: %q", tc.badValue, tc.input, got)
		}
	}
}

func TestRedact_JWT(t *testing.T) {
	jwt := `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`
	got := redact.Redact(jwt)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Errorf("JWT not redacted: %q", got)
	}
}

func TestRedact_LongHex(t *testing.T) {
	input := `trace_id: 4bf92f3577b34da6a3ce929d0e0e4736`
	got := redact.Redact(input)
	// The hex trace ID should be redacted.
	if strings.Contains(got, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("hex string not redacted: %q", got)
	}
}

func TestRedact_SafeContentUnchanged(t *testing.T) {
	safe := `{"level":"error","message":"connection refused","port":3000}`
	got := redact.Redact(safe)
	// "error", "message", "connection refused", and "port" are not secrets.
	if !strings.Contains(got, "connection refused") {
		t.Errorf("safe content was incorrectly redacted: %q", got)
	}
	if !strings.Contains(got, "level") {
		t.Errorf("safe field 'level' was incorrectly redacted: %q", got)
	}
}

func TestRedactLines_MultiLine(t *testing.T) {
	input := "normal log line\npassword=secret123\nanother normal line"
	got := redact.RedactLines(input)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	if strings.Contains(lines[1], "secret123") {
		t.Errorf("secret not redacted in line 2: %q", lines[1])
	}
	if !strings.Contains(lines[0], "normal log line") {
		t.Errorf("line 0 unexpectedly changed: %q", lines[0])
	}
	if !strings.Contains(lines[2], "another normal line") {
		t.Errorf("line 2 unexpectedly changed: %q", lines[2])
	}
}
