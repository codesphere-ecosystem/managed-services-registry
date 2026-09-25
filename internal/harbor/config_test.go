// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"strings"
	"testing"
	"time"
)

var harborEnvKeys = []string{
	"HARBOR_URL",
	"HARBOR_AUTH_MODE",
	"HARBOR_USERNAME",
	"HARBOR_PASSWORD",
	"HARBOR_TOKEN",
	"HARBOR_INSECURE_SKIP_VERIFY",
	"HARBOR_TIMEOUT_SECONDS",
	"HARBOR_PROJECT_PREFIX",
	"HARBOR_DEFAULT_ROLE_ID",
}

func setHarborEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range harborEnvKeys {
		t.Setenv(key, env[key])
	}
}

func TestLoadConfigFromEnvDefaults(t *testing.T) {
	setHarborEnv(t, map[string]string{
		"HARBOR_URL":      "https://harbor.example.com/",
		"HARBOR_USERNAME": "admin",
		"HARBOR_PASSWORD": "secret",
	})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}

	want := Config{
		BaseURL:       "https://harbor.example.com",
		AuthMode:      AuthModeBasic,
		Username:      "admin",
		Password:      "secret",
		Timeout:       30 * time.Second,
		ProjectPrefix: "ms-",
		DefaultRoleID: 2,
	}
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigFromEnvOverrides(t *testing.T) {
	setHarborEnv(t, map[string]string{
		"HARBOR_URL":                  "https://harbor.example.com",
		"HARBOR_AUTH_MODE":            "bearer",
		"HARBOR_TOKEN":                "tok",
		"HARBOR_INSECURE_SKIP_VERIFY": "true",
		"HARBOR_TIMEOUT_SECONDS":      "5",
		"HARBOR_PROJECT_PREFIX":       "cs-",
		"HARBOR_DEFAULT_ROLE_ID":      "4",
	})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.AuthMode != AuthModeBearer || cfg.Token != "tok" || !cfg.InsecureSkipVerify ||
		cfg.Timeout != 5*time.Second || cfg.ProjectPrefix != "cs-" || cfg.DefaultRoleID != 4 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadConfigFromEnvInvalidNumbersFallBack(t *testing.T) {
	setHarborEnv(t, map[string]string{
		"HARBOR_URL":                  "https://harbor.example.com",
		"HARBOR_USERNAME":             "admin",
		"HARBOR_PASSWORD":             "secret",
		"HARBOR_INSECURE_SKIP_VERIFY": "maybe",
		"HARBOR_TIMEOUT_SECONDS":      "soon",
	})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.InsecureSkipVerify || cfg.Timeout != 30*time.Second {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadConfigFromEnvErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "missing url",
			env:     map[string]string{"HARBOR_USERNAME": "admin", "HARBOR_PASSWORD": "secret"},
			wantErr: "HARBOR_URL is required",
		},
		{
			name:    "basic without password",
			env:     map[string]string{"HARBOR_URL": "https://h", "HARBOR_USERNAME": "admin"},
			wantErr: "HARBOR_USERNAME and HARBOR_PASSWORD are required",
		},
		{
			name:    "bearer without token",
			env:     map[string]string{"HARBOR_URL": "https://h", "HARBOR_AUTH_MODE": "bearer"},
			wantErr: "HARBOR_TOKEN is required",
		},
		{
			name:    "unsupported auth mode",
			env:     map[string]string{"HARBOR_URL": "https://h", "HARBOR_AUTH_MODE": "oauth"},
			wantErr: `unsupported HARBOR_AUTH_MODE "oauth"`,
		},
		{
			name: "non-positive role id",
			env: map[string]string{
				"HARBOR_URL": "https://h", "HARBOR_USERNAME": "admin", "HARBOR_PASSWORD": "secret",
				"HARBOR_DEFAULT_ROLE_ID": "0",
			},
			wantErr: "HARBOR_DEFAULT_ROLE_ID must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHarborEnv(t, tt.env)

			_, err := LoadConfigFromEnv()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
