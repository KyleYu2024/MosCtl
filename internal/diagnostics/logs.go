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

func (b *LogBuffer) Snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return compactLifecycleLogs(b.text)
}

// Keep the original detail in the bounded buffer so a failed startup can show
// its full context. Only successful lifecycle sequences are collapsed.
func compactLifecycleLogs(raw string) string {
	var result, pending strings.Builder
	active, failed := false, false
	for _, line := range strings.SplitAfter(raw, "\n") {
		info := strings.Contains(line, "\tINFO\t")
		start := info && (strings.Contains(line, "\tmain config loaded") || strings.Contains(line, "\tstarting shutdown sequences"))
		end := info && (strings.Contains(line, "\tall plugins are loaded") || strings.Contains(line, "\tall plugins were closed"))
		if start {
			result.WriteString(pending.String())
			pending.Reset()
			active, failed = true, false
		}
		if !active {
			result.WriteString(line)
			continue
		}
		pending.WriteString(line)
		if strings.Contains(line, "\tERROR\t") || strings.Contains(line, "\tFATAL\t") || strings.Contains(line, "\tPANIC\t") {
			failed = true
		}
		if end {
			if failed {
				result.WriteString(pending.String())
			} else {
				// Warnings remain visible, including the normal socket-close warning.
				for _, detail := range strings.SplitAfter(pending.String(), "\n") {
					if detail != "" && !strings.Contains(detail, "\tINFO\t") {
						result.WriteString(detail)
					}
				}
				result.WriteString(line)
			}
			pending.Reset()
			active = false
		}
	}
	result.WriteString(pending.String())
	return result.String()
}

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
	go func() {
		defer reader.Close()
		_, _ = io.Copy(&consoleStream{output: io.MultiWriter(stdout, Logs)}, reader)
	}()
	return nil
}

type consoleStream struct {
	output  io.Writer
	pending string
}

func (s *consoleStream) Write(p []byte) (int, error) {
	s.pending += string(p)
	for {
		end := strings.IndexByte(s.pending, '\n')
		if end < 0 {
			break
		}
		line := s.pending[:end]
		s.pending = s.pending[end+1:]
		if strings.Contains(line, "🚀 启动 MosDNS...") {
			QueryStats.ResetPending()
		}
		if !QueryStats.Consume(line) {
			_, _ = io.WriteString(s.output, line+"\n")
		}
	}
	if len(s.pending) > 65536 {
		_, _ = io.WriteString(s.output, s.pending)
		s.pending = ""
	}
	return len(p), nil
}
