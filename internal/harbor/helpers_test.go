// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"testing"

	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"team-a":                               "team-a",
		"Team_A":                               "team-a",
		"  My.Service  ":                       "my-service",
		"a--b__c..d":                           "a-b-c-d",
		"ünïcode/slash":                        "n-code-slash",
		"-leading-and-trailing-":               "leading-and-trailing",
		"":                                     "service",
		"!!!":                                  "service",
		"3f2b9c1e-8a4d-4f6b-9e21-7c5d0a1b2c3d": "3f2b9c1e-8a4d-4f6b-9e21-7c5d0a1b2c3d",
	}

	for input, want := range tests {
		if got := normalizeName(input); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStorageMiBToHarborQuota(t *testing.T) {
	tests := map[int]int64{
		-1:    0,
		0:     0,
		1:     1024 * 1024,
		10240: 10240 * 1024 * 1024,
	}

	for input, want := range tests {
		if got := storageMiBToHarborQuota(input); got != want {
			t.Errorf("storageMiBToHarborQuota(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestProjectRegistryURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://harbor.example.com", "https://harbor.example.com/ms-a"},
		{"https://harbor.example.com/", "https://harbor.example.com/ms-a"},
		{"https://harbor.example.com/some/path?x=1#frag", "https://harbor.example.com/ms-a"},
		{"  https://harbor.example.com:8443  ", "https://harbor.example.com:8443/ms-a"},
		{"", "ms-a"},
	}

	for _, tt := range tests {
		if got := projectRegistryURL(tt.baseURL, "ms-a"); got != tt.want {
			t.Errorf("projectRegistryURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestProjectSummaryStorage(t *testing.T) {
	const mib = 1024 * 1024

	tests := []struct {
		name        string
		summary     *harbormodels.ProjectSummary
		wantHardMiB int
		wantUsedMiB int
		wantPercent float64
	}{
		{name: "nil summary"},
		{name: "nil quota", summary: &harbormodels.ProjectSummary{}},
		{
			name:        "unlimited quota",
			summary:     summaryWith(harbormodels.ResourceList{"storage": -1}, harbormodels.ResourceList{"storage": 5 * mib}),
			wantUsedMiB: 5,
		},
		{
			name:        "missing storage keys",
			summary:     summaryWith(harbormodels.ResourceList{}, harbormodels.ResourceList{}),
			wantHardMiB: 0,
		},
		{
			name:        "partially used",
			summary:     summaryWith(harbormodels.ResourceList{"storage": 3 * mib}, harbormodels.ResourceList{"storage": 1 * mib}),
			wantHardMiB: 3,
			wantUsedMiB: 1,
			wantPercent: 33.33,
		},
		{
			name:        "empty",
			summary:     summaryWith(harbormodels.ResourceList{"storage": 10 * mib}, harbormodels.ResourceList{"storage": 0}),
			wantHardMiB: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := projectSummaryStorageMiB(tt.summary); got != tt.wantHardMiB {
				t.Errorf("storage = %d, want %d", got, tt.wantHardMiB)
			}
			if got := projectSummaryUsedStorageMiB(tt.summary); got != tt.wantUsedMiB {
				t.Errorf("used = %d, want %d", got, tt.wantUsedMiB)
			}
			if got := projectSummaryUsedStoragePercent(tt.summary); got != tt.wantPercent {
				t.Errorf("percent = %v, want %v", got, tt.wantPercent)
			}
		})
	}
}

func TestProjectRobotPermissions(t *testing.T) {
	permissions := projectRobotPermissions("ms-a")
	if len(permissions) != 1 || permissions[0].Kind != "project" || permissions[0].Namespace != "ms-a" {
		t.Fatalf("unexpected permissions: %+v", permissions)
	}

	seen := map[string]bool{}
	for _, access := range permissions[0].Access {
		key := access.Resource + ":" + access.Action
		if seen[key] {
			t.Errorf("duplicate access entry %s", key)
		}
		seen[key] = true
	}
	for _, required := range []string{"repository:pull", "repository:push", "artifact:delete", "project:read"} {
		if !seen[required] {
			t.Errorf("missing access entry %s", required)
		}
	}
}

func TestParseProjectVisibility(t *testing.T) {
	tests := []struct {
		project *harbormodels.Project
		want    bool
	}{
		{nil, false},
		{&harbormodels.Project{}, false},
		{&harbormodels.Project{Metadata: &harbormodels.ProjectMetadata{Public: "false"}}, false},
		{&harbormodels.Project{Metadata: &harbormodels.ProjectMetadata{Public: "TRUE"}}, true},
	}

	for _, tt := range tests {
		if got := parseProjectVisibility(tt.project); got != tt.want {
			t.Errorf("parseProjectVisibility(%+v) = %v, want %v", tt.project, got, tt.want)
		}
	}
}

func summaryWith(hard, used harbormodels.ResourceList) *harbormodels.ProjectSummary {
	return &harbormodels.ProjectSummary{
		Quota: &harbormodels.ProjectSummaryQuota{Hard: hard, Used: used},
	}
}
