package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnableQueryStats instruments the standard MosCtl sequences without changing their routing.
func EnableQueryStats() error {
	return updateConfigSilent(instrumentQueryStats)
}

func instrumentQueryStats(root *yaml.Node) error {
	if len(root.Content) == 0 {
		return fmt.Errorf("配置为空")
	}
	plugins := findValueNode(root, "plugins")
	if plugins == nil {
		return fmt.Errorf("缺少 plugins")
	}
	sequences := make(map[string]*yaml.Node)
	for _, plugin := range plugins.Content {
		tag := findValueNode(plugin, "tag")
		kind := findValueNode(plugin, "type")
		if tag != nil && kind != nil && kind.Value == "sequence" {
			sequences[tag.Value] = findValueNode(plugin, "args")
		}
	}
	for _, tag := range []string{"main_sequence", "cached_local_sequence", "forward_remote_upstream"} {
		if sequences[tag] == nil || sequences[tag].Kind != yaml.SequenceNode {
			return fmt.Errorf("不支持自定义配置：缺少 %s", tag)
		}
	}
	logNode := findValueNode(root, "log")
	if logNode == nil {
		return fmt.Errorf("缺少 log 配置")
	}
	if file := findValueNode(logNode, "file"); file != nil && file.Value != "" {
		return fmt.Errorf("查询统计需要控制台日志输出")
	}
	for tag, label := range map[string]string{"main_sequence": "ALL", "cached_local_sequence": "LOCAL", "forward_remote_upstream": "REMOTE"} {
		args := sequences[tag]
		found := false
		for _, step := range args.Content {
			if exec := findValueNode(step, "exec"); exec != nil && exec.Value == "query_summary MOSCTL_STATS_"+label {
				found = true
			}
		}
		if !found {
			args.Content = append([]*yaml.Node{summaryStep(label, "")}, args.Content...)
		}
	}
	main := sequences["main_sequence"]
	var updated []*yaml.Node
	for i, step := range main.Content {
		exec := findValueNode(step, "exec")
		if exec != nil && exec.Value == "$forward_local" {
			matches := findValueNode(step, "matches")
			if matches != nil && matches.Kind == yaml.ScalarNode && strings.Contains(matches.Value, "client_ip") {
				previous := (*yaml.Node)(nil)
				if len(updated) > 0 {
					previous = findValueNode(updated[len(updated)-1], "exec")
				}
				if previous == nil || previous.Value != "query_summary MOSCTL_STATS_LOCAL" {
					updated = append(updated, summaryStep("LOCAL", matches.Value))
				}
			}
		}
		updated = append(updated, step)
		if exec != nil && exec.Value == "$hosts" {
			next := (*yaml.Node)(nil)
			if i+1 < len(main.Content) {
				next = findValueNode(main.Content[i+1], "exec")
			}
			if next == nil || next.Value != "query_summary MOSCTL_STATS_HOSTS" {
				updated = append(updated, summaryStep("HOSTS", "has_resp"))
			}
		}
	}
	main.Content = updated
	if level := findValueNode(logNode, "level"); level != nil {
		level.Value = "info"
	}
	return nil
}
func summaryStep(label, match string) *yaml.Node {
	step := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if match != "" {
		step.Content = append(step.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "matches"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: match})
	}
	step.Content = append(step.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "exec"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "query_summary MOSCTL_STATS_" + label})
	return step
}
