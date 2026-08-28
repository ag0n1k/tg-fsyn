package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestRedactingWriterHidesSecrets(t *testing.T) {
	const token = "123456789:FAKE-token-value-for-tests-only-xxxxx"
	const password = "s3cret-passw0rd"

	var buf bytes.Buffer
	logger := log.New(newRedactingWriter(&buf, token, password), "", 0)

	// The shape that actually leaked: a tgbotapi network error quoting the
	// full API URL, and a Synology login URL with the password in a query
	// parameter.
	logger.Printf(`Post "https://api.telegram.org/bot%s/getUpdates": i/o timeout`, token)
	logger.Printf("login request failed: Get http://h:5000/webapi/auth.cgi?account=u&passwd=%s", password)

	out := buf.String()
	if strings.Contains(out, token) {
		t.Errorf("bot token leaked into log output: %q", out)
	}
	if strings.Contains(out, password) {
		t.Errorf("synology password leaked into log output: %q", out)
	}
	if got := strings.Count(out, redactPlaceholder); got != 2 {
		t.Errorf("expected 2 redactions, got %d in %q", got, out)
	}
	// Everything that is not a secret must survive, or the log is useless.
	if !strings.Contains(out, "getUpdates") || !strings.Contains(out, "i/o timeout") {
		t.Errorf("redaction destroyed surrounding context: %q", out)
	}
}

func TestRedactingWriterReportsCallerByteCount(t *testing.T) {
	// log.Output treats a short write as a failure, so Write must report
	// len(p) even though redaction changed the length of what it wrote.
	w := newRedactingWriter(&bytes.Buffer{}, "supersecretvalue")
	p := []byte("prefix supersecretvalue suffix\n")

	n, err := w.Write(p)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(p) {
		t.Errorf("Write returned %d, want %d", n, len(p))
	}
}

func TestRedactingWriterIgnoresShortAndEmptySecrets(t *testing.T) {
	// An unset env var must not turn every line into placeholders.
	var buf bytes.Buffer
	w := newRedactingWriter(&buf, "", "abc")

	if _, err := w.Write([]byte("abc happens to appear here\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := buf.String(); got != "abc happens to appear here\n" {
		t.Errorf("short/empty secrets should be ignored, got %q", got)
	}
}
