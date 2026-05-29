package proxy

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr               = ":8787"
	defaultUpstreamURL              = "https://api.291024.xyz"
	defaultForceServiceTier         = "priority"
	defaultAnthropicFastBeta        = "fast-mode-2026-02-01"
	defaultOpenAIJSONPaths          = "/v1/responses,/v1/chat/completions,/v1/completions"
	defaultAnthropicFastPaths       = "/v1/messages"
	defaultMaxBodyBytes       int64 = 256 << 20
)

type Config struct {
	ListenAddr         string
	UpstreamURL        *url.URL
	ForceServiceTier   string
	OpenAIJSONPaths    map[string]struct{}
	AnthropicFastPaths map[string]struct{}
	AnthropicFastBeta  string
	MaxBodyBytes       int64
	StrictInjection    bool
	ReadHeaderTimeout  time.Duration
	IdleTimeout        time.Duration
	MaxHeaderBytes     int
}

func LoadConfigFromEnv() (Config, error) {
	rawUpstream := envString("UPSTREAM_URL", defaultUpstreamURL)
	upstream, err := url.Parse(rawUpstream)
	if err != nil {
		return Config{}, fmt.Errorf("parse UPSTREAM_URL: %w", err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return Config{}, fmt.Errorf("UPSTREAM_URL must use http or https")
	}
	if upstream.Host == "" {
		return Config{}, fmt.Errorf("UPSTREAM_URL must include host")
	}

	tier := normalizeServiceTier(envString("FORCE_SERVICE_TIER", defaultForceServiceTier))
	if tier == "" {
		return Config{}, fmt.Errorf("FORCE_SERVICE_TIER must be one of priority, fast, flex, auto, default, scale")
	}

	maxBodyBytes, err := parseBytes(envString("MAX_BODY_BYTES", strconv.FormatInt(defaultMaxBodyBytes, 10)))
	if err != nil {
		return Config{}, fmt.Errorf("parse MAX_BODY_BYTES: %w", err)
	}
	if maxBodyBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_BODY_BYTES must be positive")
	}

	return Config{
		ListenAddr:         envString("LISTEN_ADDR", defaultListenAddr),
		UpstreamURL:        upstream,
		ForceServiceTier:   tier,
		OpenAIJSONPaths:    parsePathSet(envString("OPENAI_JSON_PATHS", defaultOpenAIJSONPaths)),
		AnthropicFastPaths: parsePathSet(envString("ANTHROPIC_FAST_PATHS", defaultAnthropicFastPaths)),
		AnthropicFastBeta:  envString("ANTHROPIC_FAST_BETA", defaultAnthropicFastBeta),
		MaxBodyBytes:       maxBodyBytes,
		StrictInjection:    envBool("STRICT_INJECTION", true),
		ReadHeaderTimeout:  envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		IdleTimeout:        envDuration("IDLE_TIMEOUT", 120*time.Second),
		MaxHeaderBytes:     int(envBytesOrDefault("MAX_HEADER_BYTES", 1<<20)),
	}, nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBytesOrDefault(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := parseBytes(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parsePathSet(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		path := normalizePath(item)
		if path != "" {
			out[path] = struct{}{}
		}
	}
	return out
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func normalizeServiceTier(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "fast" {
		value = "priority"
	}
	switch value {
	case "priority", "flex", "auto", "default", "scale":
		return value
	default:
		return ""
	}
}

func parseBytes(raw string) (int64, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return 0, fmt.Errorf("empty value")
	}

	multiplier := int64(1)
	for _, suffix := range []struct {
		text string
		mul  int64
	}{
		{"gib", 1 << 30},
		{"gb", 1 << 30},
		{"g", 1 << 30},
		{"mib", 1 << 20},
		{"mb", 1 << 20},
		{"m", 1 << 20},
		{"kib", 1 << 10},
		{"kb", 1 << 10},
		{"k", 1 << 10},
	} {
		if strings.HasSuffix(value, suffix.text) {
			multiplier = suffix.mul
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix.text))
			break
		}
	}

	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return n * multiplier, nil
}
