package diagnostics

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

const maxBytes = 256 * 1024

type LogBuffer struct {
	mu   sync.Mutex
	text string
}

var Logs = &LogBuffer{}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > maxBytes {
		b.text = b.text[len(b.text)-maxBytes:]
		if i := strings.IndexByte(b.text, '\n'); i >= 0 {
			b.text = b.text[i+1:]
		}
	}
	return len(p), nil
}

func (b *LogBuffer) Snapshot() string { b.mu.Lock(); defer b.mu.Unlock(); return b.text }

// Capture preserves Docker console output while retaining a bounded in-memory tail.
func Capture() error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	stdout := os.Stdout
	os.Stdout = writer
	os.Stderr = writer
	log.SetOutput(writer)
	go func() { defer reader.Close(); _, _ = io.Copy(io.MultiWriter(stdout, Logs), reader) }()
	return nil
}
