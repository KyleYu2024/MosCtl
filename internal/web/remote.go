package web

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func remoteNode(data []byte) (*yaml.Node, *yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}
	if len(root.Content) == 0 {
		return nil, nil, fmt.Errorf("配置为空")
	}
	plugins := mappingValue(root.Content[0], "plugins")
	if plugins != nil {
		for _, plugin := range plugins.Content {
			tag := mappingValue(plugin, "tag")
			if tag == nil || tag.Value != "forward_remote" {
				continue
			}
			upstreams := mappingValue(mappingValue(plugin, "args"), "upstreams")
			if upstreams != nil && len(upstreams.Content) > 0 {
				addr := mappingValue(upstreams.Content[0], "addr")
				if addr != nil && addr.Kind == yaml.ScalarNode {
					return &root, addr, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("未找到 forward_remote 的上游地址")
}

func validateRemote(input string) (string, error) {
	input = strings.TrimSpace(input)
	if !strings.Contains(input, "://") {
		input = "udp://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("请输入有效 DNS 地址，不支持用户名和密码")
	}
	switch u.Scheme {
	case "udp", "tcp", "tls", "https":
	default:
		return "", fmt.Errorf("支持 udp://、tcp://、tls:// 和 https://")
	}
	if u.Scheme != "https" && (u.Path != "" || u.RawQuery != "") {
		return "", fmt.Errorf("该协议地址不支持路径或查询参数")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("端口必须在 1–65535 之间")
		}
	}
	if net.ParseIP(u.Hostname()) == nil {
		if _, err := testDomain(u.Hostname()); err != nil {
			return "", err
		}
	}
	return input, nil
}

func (s *Server) getRemote(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.ruleDir), "config.yaml"))
	if err != nil {
		writeError(w, 500, "无法读取 MosDNS 配置")
		return
	}
	_, addr, err := remoteNode(data)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"remote": addr.Value})
}

func (s *Server) saveRemote(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, 403, "请求来源无效")
		return
	}
	var req struct {
		Remote string `json:"remote"`
	}
	if decodeJSON(r, &req, 4096) != nil {
		writeError(w, 400, "地址无效")
		return
	}
	addrValue, err := validateRemote(req.Remote)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	configPath := filepath.Join(filepath.Dir(s.ruleDir), "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		writeError(w, 500, "无法读取 MosDNS 配置")
		return
	}
	root, addr, err := remoteNode(data)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if addr.Value == addrValue && strings.Contains(addr.LineComment, "WEB_MANAGED") {
		writeJSON(w, 200, map[string]any{"remote": addrValue, "message": "配置未改变"})
		return
	}
	addr.Value = addrValue
	if !strings.Contains(addr.LineComment, "TAG_REMOTE") {
		addr.LineComment += " TAG_REMOTE"
	}
	if !strings.Contains(addr.LineComment, "WEB_MANAGED") {
		addr.LineComment += " WEB_MANAGED"
	}
	updated, err := yaml.Marshal(root)
	if err != nil {
		writeError(w, 500, "配置编码失败")
		return
	}
	file, err := os.CreateTemp(filepath.Dir(configPath), ".remote-config-*")
	if err != nil {
		writeError(w, 500, "无法保存配置")
		return
	}
	defer os.Remove(file.Name())
	mode := os.FileMode(0644)
	if info, e := os.Stat(configPath); e == nil {
		mode = info.Mode().Perm()
	}
	err = file.Chmod(mode)
	if err == nil {
		_, err = file.Write(updated)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), configPath)
	}
	if err != nil {
		writeError(w, 500, "保存配置失败")
		return
	}
	if err = s.restart(); err != nil {
		writeJSON(w, 500, map[string]any{"saved": true, "error": "配置已保存，但重载请求失败"})
		return
	}
	writeJSON(w, 200, map[string]any{"remote": addrValue, "message": "REMOTE 已保存，已请求重载 MosDNS"})
}
