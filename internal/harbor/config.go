// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeoutSeconds = 30
	defaultProjectPrefix  = "ms-"
	defaultRoleID         = 2
)

// AuthMode defines how the provider authenticates against Harbor.
type AuthMode string

const (
	AuthModeBasic  AuthMode = "basic"
	AuthModeBearer AuthMode = "bearer"
)

// Config holds Harbor provider configuration.
type Config struct {
	BaseURL            string
	AuthMode           AuthMode
	Username           string
	Password           string
	Token              string
	InsecureSkipVerify bool
	Timeout            time.Duration
	ProjectPrefix      string
	DefaultRoleID      int
}

// LoadConfigFromEnv loads Harbor configuration from environment variables.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:            strings.TrimRight(os.Getenv("HARBOR_URL"), "/"),
		AuthMode:           AuthMode(getEnv("HARBOR_AUTH_MODE", string(AuthModeBasic))),
		Username:           os.Getenv("HARBOR_USERNAME"),
		Password:           os.Getenv("HARBOR_PASSWORD"),
		Token:              os.Getenv("HARBOR_TOKEN"),
		InsecureSkipVerify: getEnvBool("HARBOR_INSECURE_SKIP_VERIFY", false),
		Timeout:            time.Duration(getEnvInt("HARBOR_TIMEOUT_SECONDS", defaultTimeoutSeconds)) * time.Second,
		ProjectPrefix:      getEnv("HARBOR_PROJECT_PREFIX", defaultProjectPrefix),
		DefaultRoleID:      getEnvInt("HARBOR_DEFAULT_ROLE_ID", defaultRoleID),
	}

	if cfg.BaseURL == "" {
		return Config{}, errors.New("HARBOR_URL is required")
	}

	switch cfg.AuthMode {
	case AuthModeBasic:
		if cfg.Username == "" || cfg.Password == "" {
			return Config{}, errors.New("HARBOR_USERNAME and HARBOR_PASSWORD are required for basic auth")
		}
	case AuthModeBearer:
		if cfg.Token == "" {
			return Config{}, errors.New("HARBOR_TOKEN is required for bearer auth")
		}
	default:
		return Config{}, fmt.Errorf("unsupported HARBOR_AUTH_MODE %q", cfg.AuthMode)
	}

	if cfg.ProjectPrefix == "" {
		return Config{}, errors.New("HARBOR_PROJECT_PREFIX must not be empty")
	}
	if cfg.DefaultRoleID <= 0 {
		return Config{}, errors.New("HARBOR_DEFAULT_ROLE_ID must be positive")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}
