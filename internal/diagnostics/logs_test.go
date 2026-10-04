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

func TestLifecycleLogsCollapseSuccessfulSequences(t *testing.T) {
	raw := "time\tINFO\tmain config loaded\n" +
		"time\tINFO\tloading plugin\t{\"tag\":\"cache\"}\n" +
		"time\tINFO\tall plugins are loaded\n" +
		"time\tINFO\tstarting shutdown sequences\n" +
		"time\tINFO\tclosing plugin\n" +
		"time\tWARN\tudp_server\tread err\n" +
		"time\tINFO\tall plugins were closed\n"
	want := "time\tINFO\tall plugins are loaded\n" +
		"time\tWARN\tudp_server\tread err\n" +
		"time\tINFO\tall plugins were closed\n"
	if got := compactLifecycleLogs(raw); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLifecycleLogsKeepFailureAndIncompleteContext(t *testing.T) {
	for _, ending := range []string{"", "time\tINFO\tall plugins are loaded\n"} {
		raw := "time\tINFO\tmain config loaded\n" +
			"time\tINFO\tloading plugin\n" +
			"time\tERROR\tplugin failed\n" + ending
		if got := compactLifecycleLogs(raw); got != raw {
			t.Fatalf("lost failure context: %q", got)
		}
	}
	raw := "time\tINFO\tmain config loaded\ntime\tINFO\tloading plugin\n"
	if got := compactLifecycleLogs(raw); got != raw {
		t.Fatalf("lost incomplete startup: %q", got)
	}
}
