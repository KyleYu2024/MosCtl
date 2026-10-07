package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// OptimizeStandard migrates the standard template; unfamiliar sequences are left untouched.
func OptimizeStandard() error { return updateConfigSilent(optimizeStandard) }

func optimizeStandard(root *yaml.Node) error {
	plugins := findValueNode(root, "plugins")
	if plugins == nil || plugins.Kind != yaml.SequenceNode {
		return fmt.Errorf("缺少标准插件配置")
	}
	tags := make(map[string]*yaml.Node)
	for _, p := range plugins.Content {
		if tag := findValueNode(p, "tag"); tag != nil {
			tags[tag.Value] = p
		}
	}
	main := findValueNode(tags["main_sequence"], "args")
	if main == nil || main.Kind != yaml.SequenceNode {
		return fmt.Errorf("缺少标准主序列")
	}
	anchor := -1
	for i, step := range main.Content {
		if exec := findValueNode(step, "exec"); exec != nil && exec.Value == "$query_is_apple_domain" {
			anchor = i
			break
		}
	}
	if anchor < 0 || tags["cached_local_sequence"] == nil || tags["forward_remote_upstream"] == nil {
		return fmt.Errorf("自定义分流序列，未自动迁移")
	}
	for _, rule := range []struct{ tag, source, file, sequence string }{
		{"mosctl_force_cn", "geosite_cn", "force-cn.txt", "cached_local_sequence"},
		{"mosctl_force_nocn", "geosite_no_cn", "force-nocn.txt", "forward_remote_upstream"},
	} {
		if tags[rule.tag] != nil {
			continue
		}
		files := findValueNode(tags[rule.source], "files")
		path := ""
		if files != nil {
			for _, f := range files.Content {
				if strings.HasSuffix(f.Value, "/"+rule.file) {
					path = f.Value
				}
			}
		}
		if path == "" {
			return fmt.Errorf("缺少 %s，未自动迁移", rule.file)
		}
		var plugin yaml.Node
		if err := yaml.Unmarshal([]byte(fmt.Sprintf("tag: %s\ntype: domain_set\nargs:\n  files:\n    - %q\n", rule.tag, path)), &plugin); err != nil {
			return err
		}
		// MosDNS resolves matchers in load order, so domain sets must precede main_sequence.
		for i, p := range plugins.Content {
			if p == tags["main_sequence"] {
				plugins.Content = append(plugins.Content[:i], append([]*yaml.Node{plugin.Content[0]}, plugins.Content[i:]...)...)
				break
			}
		}
		var step yaml.Node
		if err := yaml.Unmarshal([]byte(fmt.Sprintf("matches: qname $%s\nexec: $%s\n", rule.tag, rule.sequence)), &step); err != nil {
			return err
		}
		var accept yaml.Node
		_ = yaml.Unmarshal([]byte("exec: jump has_resp_sequence\n"), &accept)
		main.Content = append(main.Content[:anchor], append([]*yaml.Node{step.Content[0], accept.Content[0]}, main.Content[anchor:]...)...)
		anchor += 2
	}
	// cache saves the downstream response while unwinding; the trailing invocation is redundant.
	args := findValueNode(tags["cached_local_sequence"], "args")
	if args != nil {
		var meaningful []*yaml.Node
		for _, step := range args.Content {
			exec := findValueNode(step, "exec")
			if exec != nil && !strings.HasPrefix(exec.Value, "query_summary ") {
				meaningful = append(meaningful, step)
			}
		}
		if len(meaningful) == 4 && nodeExec(meaningful[0]) == "$cache" && nodeExec(meaningful[1]) == "accept" && nodeExec(meaningful[2]) == "$forward_local" && nodeExec(meaningful[3]) == "$cache" {
			match := findValueNode(meaningful[1], "matches")
			if match != nil && match.Value == "has_resp" {
				var updated []*yaml.Node
				for _, step := range args.Content {
					if step != meaningful[3] {
						updated = append(updated, step)
					}
				}
				args.Content = updated
			}
		}
	}
	return nil
}

func nodeExec(n *yaml.Node) string {
	if v := findValueNode(n, "exec"); v != nil {
		return v.Value
	}
	return ""
}
