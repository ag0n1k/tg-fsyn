package main

import (
	"io"
	"strings"
	"sync"
)

// redactPlaceholder is what a secret is replaced with in the log.
const redactPlaceholder = "[REDACTED]"

// minRedactedSecretLen guards against blanking half the log: a very short
// value (an empty or one-character env var) would match unrelated text.
const minRedactedSecretLen = 8

// redactingWriter wraps a log destination and strips known secrets from
// every line before it is written.
//
// It exists because our dependencies leak credentials through ordinary
// error strings, so no amount of care at individual log call sites is
// enough: tgbotapi's network errors quote the full API URL, which embeds
// the bot token, and the Synology auth endpoint takes the password as a
// query parameter (see the login URL built in synology.go). During the DNS
// outage on 2026-08-28 that wrote the bot token into tg-fsyn.log around
// four thousand times in a single day.
//
// Redacting at the writer also covers log output from inside tgbotapi
// itself (the "Failed to get updates" retry lines are its own).
type redactingWriter struct {
	dst     io.Writer
	secrets []string
	mu      sync.Mutex
}

// newRedactingWriter returns a writer that hides secrets on the way to
// dst. Secrets shorter than minRedactedSecretLen are ignored.
func newRedactingWriter(dst io.Writer, secrets ...string) *redactingWriter {
	w := &redactingWriter{dst: dst}
	for _, s := range secrets {
		if len(s) >= minRedactedSecretLen {
			w.secrets = append(w.secrets, s)
		}
	}
	return w
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.secrets) == 0 {
		return w.dst.Write(p)
	}

	s := string(p)
	for _, secret := range w.secrets {
		s = strings.ReplaceAll(s, secret, redactPlaceholder)
	}

	if _, err := io.WriteString(w.dst, s); err != nil {
		return 0, err
	}
	// Report the caller's byte count, not ours: redaction changes the
	// length, and log.Output treats a short write as an error.
	return len(p), nil
}
