package web

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleYu2024/mosctl/internal/ruleformat"
	"github.com/KyleYu2024/mosctl/internal/service"
)

func applyDNS(ctx context.Context) error {
	if err := service.RestartAndWait(ctx); err != nil {
		return err
	}
	return kernelDNSHealth(ctx)
}

// Caller holds OperationMu.
func (s *Server) applyFile(ctx context.Context, path string, data []byte, apply bool) error {
	if apply {
		return service.ApplyFiles(ctx, map[string][]byte{path: data}, s.apply)
	}
	backup, err := service.BackupFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(backup.Data, data) && backup.Existed {
		return nil
	}
	return service.WriteManaged(path, data, backup.Mode)
}

func validateRule(id, content string) error { return ruleformat.Validate(id, content) }

func (s *Server) checkRuleConflict(id, content string) error {
	opposite := ""
	if id == "force-cn" {
		opposite = "force-nocn.txt"
	} else if id == "force-nocn" {
		opposite = "force-cn.txt"
	} else {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.ruleDir, opposite))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("无法读取另一分流规则：%w", err)
	}
	keys := make(map[string]bool)
	key := func(v string) string {
		return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(strings.SplitN(v, "#", 2)[0])), ".")
	}
	for _, line := range strings.Split(string(data), "\n") {
		k := key(line)
		if k != "" {
			keys[k] = true
		}
	}
	for i, line := range strings.Split(content, "\n") {
		if k := key(line); k != "" && keys[k] {
			return fmt.Errorf("第 %d 行：%s 同时出现在国内、国外自定义规则中", i+1, k)
		}
	}
	return nil
}
