// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientUsesAPIBasePath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AuthMode: AuthModeBasic})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v2.0/projects" {
		t.Fatalf("path = %q, want /api/v2.0/projects", gotPath)
	}
}
