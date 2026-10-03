package config

import (
	"bytes"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInstrumentationIsIdempotent(t *testing.T) {
	data, err := os.ReadFile("../../templates/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err = yaml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if err = instrumentQueryStats(&root); err != nil {
		t.Fatal(err)
	}
	first, _ := yaml.Marshal(&root)
	if err = instrumentQueryStats(&root); err != nil {
		t.Fatal(err)
	}
	second, _ := yaml.Marshal(&root)
	if !bytes.Equal(first, second) {
		t.Fatal("instrumentation duplicated on restart")
	}
	for _, marker := range []string{"MOSCTL_STATS_ALL", "MOSCTL_STATS_LOCAL", "MOSCTL_STATS_REMOTE", "MOSCTL_STATS_HOSTS"} {
		if !bytes.Contains(first, []byte(marker)) {
			t.Fatal("missing", marker)
		}
	}
}
