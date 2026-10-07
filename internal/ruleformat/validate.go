package ruleformat

import (
	"fmt"
	"github.com/miekg/dns"
	"net"
	"regexp"
	"strings"
)

func Validate(id, content string) error {
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		var err error
		switch id {
		case "user-iot":
			if net.ParseIP(line) == nil {
				if _, _, e := net.ParseCIDR(line); e != nil {
					err = fmt.Errorf("请输入有效 IP 或 CIDR")
				}
			}
		case "hosts":
			fields := strings.Fields(line)
			if len(fields) < 2 {
				err = fmt.Errorf("Hosts 格式为 域名 IP [IP…]")
			} else {
				err = ValidateDomain(fields[0])
				for _, ip := range fields[1:] {
					if net.ParseIP(ip) == nil {
						err = fmt.Errorf("无效 IP：%s", ip)
						break
					}
				}
			}
		default:
			err = ValidateDomain(line)
		}
		if err != nil {
			return fmt.Errorf("第 %d 行：%w", i+1, err)
		}
	}
	return nil
}

func ValidateDomain(rule string) error {
	if strings.HasPrefix(rule, "regexp:") {
		_, err := regexp.Compile(strings.TrimPrefix(rule, "regexp:"))
		return err
	}
	if strings.HasPrefix(rule, "keyword:") {
		if strings.TrimPrefix(rule, "keyword:") == "" {
			return fmt.Errorf("keyword 不能为空")
		}
		return nil
	}
	name := rule
	for _, prefix := range []string{"full:", "domain:"} {
		name = strings.TrimPrefix(name, prefix)
	}
	if _, ok := dns.IsDomainName(name); !ok || strings.ContainsAny(name, " /:@<>\\") || net.ParseIP(name) != nil {
		return fmt.Errorf("无效域名规则：%s", rule)
	}
	return nil
}
