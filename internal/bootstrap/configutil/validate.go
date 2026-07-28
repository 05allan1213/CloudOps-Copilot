// Package configutil 提供环境变量读取、类型转换和配置校验工具函数。
package configutil

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func ValidateHTTPURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid http or https URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https scheme, got %q", name, parsed.Scheme)
	}
	if parsed.Port() != "" {
		if err := ValidatePort(name, parsed.Port()); err != nil {
			return err
		}
	}
	return nil
}

func ValidateListenAddr(name, raw string) error {
	return ValidateHostPort(name, raw)
}

func ValidateHostPort(name, raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s must use host:port format: %w", name, err)
	}
	if strings.TrimSpace(host) != host || strings.TrimSpace(port) != port {
		return fmt.Errorf("%s must not contain surrounding spaces", name)
	}
	return ValidatePort(name, port)
}

func ValidatePort(name, raw string) error {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be in range 1-65535, got %q", name, raw)
	}
	return nil
}
