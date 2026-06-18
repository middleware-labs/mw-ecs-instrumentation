package cmd

import (
	"bufio"
	"os"
	"strings"
)

const mwConfigPath = "/etc/mw-agent/mw-ecs.conf"

func loadMWConfig() map[string]string {
	cfg := make(map[string]string)

	f, err := os.Open(mwConfigPath)
	if err != nil {
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cfg[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return cfg
}
