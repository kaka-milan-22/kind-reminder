package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerPort string
	DBPath     string
	APIToken   string

	TelegramBotToken string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	QueueConfig QueueConfig

	SchedulerMaxLateness time.Duration

	Webhook WebhookConfig

	// Lists holds block-sequence values from the YAML config, keyed by dotted
	// path (e.g. "webhook.urls" -> ["https://a", "https://b"]). It is the
	// access point for list-valued keys; typed fields can read from it as
	// needed.
	Lists map[string][]string
}

type QueueConfig struct {
	Type            string
	Workers         int
	Size            int
	RateLimitPerSec int
}

type WebhookConfig struct {
	Enabled bool
	BaseURL string
	Timeout int
}

func Load() (Config, error) {
	cfg := defaultConfig()

	yamlPath := getEnv("CONFIG_FILE", "./config.yaml")
	if err := applyYAMLFile(&cfg, yamlPath); err != nil {
		return Config{}, err
	}

	applyEnv(&cfg)

	if cfg.APIToken == "" {
		return Config{}, fmt.Errorf("API_TOKEN is required (or set api_token in YAML)")
	}
	if cfg.QueueConfig.Type == "" {
		cfg.QueueConfig.Type = "memory"
	}
	if cfg.QueueConfig.Workers <= 0 {
		cfg.QueueConfig.Workers = 10
	}
	if cfg.QueueConfig.Size <= 0 {
		cfg.QueueConfig.Size = 100
	}
	return cfg, nil
}

func defaultConfig() Config {
	cfg := Config{
		ServerPort: "8080",
		DBPath:     "./reminder.db",
		SMTPPort:   587,
		QueueConfig: QueueConfig{
			Type:    "memory",
			Workers: 10,
			Size:    100,
		},
		SchedulerMaxLateness: time.Minute,
		Webhook: WebhookConfig{
			Timeout: 5,
		},
	}
	return cfg
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("SERVER_PORT"); v != "" {
		cfg.ServerPort = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("API_TOKEN"); v != "" {
		cfg.APIToken = v
	}
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.TelegramBotToken = v
	}
	if v := os.Getenv("SMTP_HOST"); v != "" {
		cfg.SMTPHost = v
	}
	if v := os.Getenv("SMTP_USER"); v != "" {
		cfg.SMTPUser = v
	}
	if v := os.Getenv("SMTP_PASS"); v != "" {
		cfg.SMTPPass = v
	}
	if v := os.Getenv("SMTP_FROM"); v != "" {
		cfg.SMTPFrom = v
	}
	if v := parseEnvInt("SMTP_PORT"); v > 0 {
		cfg.SMTPPort = v
	}
	if v := parseEnvInt("WORKERS"); v > 0 {
		cfg.QueueConfig.Workers = v
	}
	if v := parseEnvInt("QUEUE_SIZE"); v > 0 {
		cfg.QueueConfig.Size = v
	}
	if v := os.Getenv("QUEUE_TYPE"); v != "" {
		cfg.QueueConfig.Type = strings.TrimSpace(v)
	}
	if v := parseEnvInt("QUEUE_WORKERS"); v > 0 {
		cfg.QueueConfig.Workers = v
	}
	if v := parseEnvInt("RATE_LIMIT_PER_SEC"); v > 0 {
		cfg.QueueConfig.RateLimitPerSec = v
	}
	if v := os.Getenv("WEBHOOK_BASE_URL"); v != "" {
		cfg.Webhook.BaseURL = v
	}
	if v := parseEnvInt("WEBHOOK_TIMEOUT_SECONDS"); v > 0 {
		cfg.Webhook.Timeout = v
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("WEBHOOK_ENABLED"))); v != "" {
		cfg.Webhook.Enabled = v == "1" || v == "true" || v == "yes" || v == "on"
	}
}

func applyYAMLFile(cfg *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open config file %q: %w", path, err)
	}
	defer f.Close()

	flat, lists, err := parseSimpleYAML(f)
	if err != nil {
		return fmt.Errorf("parse config file %q: %w", path, err)
	}
	if len(lists) > 0 {
		if cfg.Lists == nil {
			cfg.Lists = make(map[string][]string, len(lists))
		}
		for k, v := range lists {
			cfg.Lists[k] = v
		}
	}

	if v := flat["server_port"]; v != "" {
		cfg.ServerPort = v
	}
	if v := flat["db_path"]; v != "" {
		cfg.DBPath = v
	}
	if v := flat["api_token"]; v != "" {
		cfg.APIToken = v
	}
	if v := flat["telegram.bot_token"]; v != "" {
		cfg.TelegramBotToken = v
	}
	if v := flat["smtp.host"]; v != "" {
		cfg.SMTPHost = v
	}
	if v := flat["smtp.user"]; v != "" {
		cfg.SMTPUser = v
	}
	if v := flat["smtp.pass"]; v != "" {
		cfg.SMTPPass = v
	}
	if v := flat["smtp.from"]; v != "" {
		cfg.SMTPFrom = v
	}
	if v, ok := parseInt(flat["smtp.port"]); ok && v > 0 {
		cfg.SMTPPort = v
	}
	if v, ok := parseDuration(flat["scheduler.max_lateness"]); ok {
		cfg.SchedulerMaxLateness = v
	}
	if v := flat["queue.type"]; v != "" {
		cfg.QueueConfig.Type = v
	}
	if v, ok := parseInt(flat["queue.workers"]); ok && v > 0 {
		cfg.QueueConfig.Workers = v
	}
	if v, ok := parseInt(flat["queue.size"]); ok && v > 0 {
		cfg.QueueConfig.Size = v
	}
	if v, ok := parseInt(flat["queue.rate_limit_per_sec"]); ok && v > 0 {
		cfg.QueueConfig.RateLimitPerSec = v
	}
	if v := flat["webhook.base_url"]; v != "" {
		cfg.Webhook.BaseURL = v
	}
	if v, ok := parseInt(flat["webhook.timeout_seconds"]); ok && v > 0 {
		cfg.Webhook.Timeout = v
	}
	if v := strings.ToLower(strings.TrimSpace(flat["webhook.enabled"])); v != "" {
		cfg.Webhook.Enabled = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	return nil
}

// parseSimpleYAML flattens a small YAML subset into dotted keys. Scalars land
// in the first map (e.g. "smtp.host" -> "mail"); sequences land in the second
// (e.g. a "webhook.urls:" key followed by "- a"/"- b" -> {"webhook.urls": [a, b]}).
// It supports nested maps with 2-space indentation, quoted scalars, and block
// sequences; it does not support inline flow syntax ([a, b]) or lists of maps.
func parseSimpleYAML(file *os.File) (map[string]string, map[string][]string, error) {
	scalars := map[string]string{}
	lists := map[string][]string{}
	stack := make([]string, 0, 4)
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := leadingSpaces(line)
		if indent%2 != 0 {
			return nil, nil, fmt.Errorf("line %d: indentation must use multiples of 2 spaces", lineNo)
		}
		level := indent / 2

		// Block sequence item: belongs to the most recent key (the current
		// stack), which must have been opened with an empty value.
		if trimmed == "-" || strings.HasPrefix(trimmed, "- ") {
			if len(stack) == 0 {
				return nil, nil, fmt.Errorf("line %d: list item without a parent key", lineNo)
			}
			key := strings.Join(stack, ".")
			if _, taken := scalars[key]; taken {
				return nil, nil, fmt.Errorf("line %d: %q already has a scalar value", lineNo, key)
			}
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			lists[key] = append(lists[key], unquote(item))
			continue
		}

		if level > len(stack) {
			return nil, nil, fmt.Errorf("line %d: invalid indentation level", lineNo)
		}
		stack = stack[:level]

		key, raw, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, nil, fmt.Errorf("line %d: expected key:value", lineNo)
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if key == "" {
			return nil, nil, fmt.Errorf("line %d: empty key", lineNo)
		}

		if raw == "" {
			stack = append(stack, key)
			continue
		}

		path := append(append([]string{}, stack...), key)
		scalars[strings.Join(path, ".")] = unquote(raw)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return scalars, lists, nil
}

func leadingSpaces(s string) int {
	n := 0
	for _, ch := range s {
		if ch != ' ' {
			break
		}
		n++
	}
	return n
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func parseEnvInt(k string) int {
	v := os.Getenv(k)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func parseDuration(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false
	}
	return d, true
}

func parseInt(v string) (int, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func getEnv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
