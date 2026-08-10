package main

import (
	"fmt"
	"os"
	"strings"
)

// Config — все настройки программы из config.txt
type Config struct {
	Login     string
	Password  string
	RFIPath   string
	IDPath    string
	Theme     string
	LastTasks string
}

// IsComplete — заполнен ли минимум, необходимый для работы (RFI)
func (c *Config) IsComplete() bool {
	return c.Login != "" && c.Password != "" && c.RFIPath != ""
}

// LoadConfig читает config.txt и автоматически мигрирует старый формат
// (строка network_path= старого формата становится rfi_path=)
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать config.txt: %v", err)
	}

	cfg := &Config{}
	hasOld := false
	hasNew := false

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "login="):
			cfg.Login = strings.TrimPrefix(line, "login=")
		case strings.HasPrefix(line, "password="):
			cfg.Password = strings.TrimPrefix(line, "password=")
		case strings.HasPrefix(line, "rfi_path="):
			cfg.RFIPath = strings.TrimPrefix(line, "rfi_path=")
			hasNew = true
		case strings.HasPrefix(line, "id_path="):
			cfg.IDPath = strings.TrimPrefix(line, "id_path=")
		case strings.HasPrefix(line, "theme="):
			cfg.Theme = strings.TrimPrefix(line, "theme=")
		case strings.HasPrefix(line, "last_tasks="):
			cfg.LastTasks = strings.TrimPrefix(line, "last_tasks=")
		case strings.HasPrefix(line, "network_path="):
			hasOld = true
			if cfg.RFIPath == "" {
				cfg.RFIPath = strings.TrimPrefix(line, "network_path=")
			}
		}
	}

	// Автоматическая миграция старого формата в новый
	if hasOld && !hasNew && cfg.RFIPath != "" {
		_ = SaveConfig(path, cfg)
	}

	return cfg, nil
}

// SaveConfig записывает config.txt в фиксированном порядке
func SaveConfig(path string, cfg *Config) error {
	var b strings.Builder
	b.WriteString("login=" + cfg.Login + "\n")
	b.WriteString("password=" + cfg.Password + "\n")
	b.WriteString("rfi_path=" + cfg.RFIPath + "\n")
	b.WriteString("id_path=" + cfg.IDPath + "\n")
	b.WriteString("theme=" + cfg.Theme + "\n")
	b.WriteString("last_tasks=" + cfg.LastTasks + "\n")
	return os.WriteFile(path, []byte(b.String()), 0644)
}
