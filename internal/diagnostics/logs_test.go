package diagnostics

import (
	"strings"
	"testing"
)

func TestLogBufferKeepsBoundedTail(t *testing.T) {
	b := &LogBuffer{}
	b.Write([]byte(strings.Repeat("old line\n", 40000)))
	b.Write([]byte("newest line\n"))
	snapshot := b.Snapshot()
	if len(snapshot) > maxBytes || !strings.HasSuffix(snapshot, "newest line\n") {
		t.Fatalf("unexpected tail size=%d", len(snapshot))
	}
}
