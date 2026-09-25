// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientUsesAPIBasePath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:  server.URL + "/",
		AuthMode: AuthModeBasic,
		Username: "admin",
		Password: "secret",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.ListProjects(context.Background()); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if gotPath != "/api/v2.0/projects" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/v2.0/projects")
	}
}
