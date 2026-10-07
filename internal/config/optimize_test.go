package config

import (
	"bytes"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStandardMigrationPreservesPolicyAndIsIdempotent(t *testing.T) {
	data, err := os.ReadFile("testdata/legacy_config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err = yaml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if err = optimizeStandard(&root); err != nil {
		t.Fatal(err)
	}
	if err = instrumentQueryStats(&root); err != nil {
		t.Fatal(err)
	}
	first, _ := yaml.Marshal(&root)
	if err = optimizeStandard(&root); err != nil {
		t.Fatal(err)
	}
	if err = instrumentQueryStats(&root); err != nil {
		t.Fatal(err)
	}
	second, _ := yaml.Marshal(&root)
	if !bytes.Equal(first, second) {
		t.Fatal("migration not idempotent")
	}
	plugins := findValueNode(&root, "plugins")
	var main, cached *yaml.Node
	mainIndex, forceIndex := 0, 0
	for i, p := range plugins.Content {
		if findValueNode(p, "tag").Value == "main_sequence" {
			mainIndex = i
		}
		if findValueNode(p, "tag").Value == "mosctl_force_nocn" {
			forceIndex = i
		}
		tag := findValueNode(p, "tag")
		if tag.Value == "main_sequence" {
			main = findValueNode(p, "args")
		}
		if tag.Value == "cached_local_sequence" {
			cached = findValueNode(p, "args")
		}
	}
	if forceIndex >= mainIndex {
		t.Fatal("force matcher loaded after main sequence")
	}
	position := map[string]int{}
	for i, step := range main.Content {
		if match := findValueNode(step, "matches"); match != nil {
			position[match.Value] = i
		}
		position[nodeExec(step)] = i
	}
	if position["qname $mosctl_force_nocn"] >= position["$query_is_apple_domain"] || position["qname $mosctl_force_cn"] >= position["$query_is_local_domain"] {
		t.Fatal("custom rules do not take priority")
	}
	cacheCalls := 0
	for _, step := range cached.Content {
		if nodeExec(step) == "$cache" {
			cacheCalls++
		}
	}
	if cacheCalls != 1 {
		t.Fatalf("cache calls=%d", cacheCalls)
	}
	if !bytes.Contains(first, []byte("reject 3")) || !bytes.Contains(first, []byte("lazy_cache_ttl: 86400")) || !bytes.Contains(first, []byte("MOSCTL_STATS_REJECTED")) {
		t.Fatal("policy changed or reject marker missing")
	}
}
