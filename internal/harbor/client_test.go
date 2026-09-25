// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/codesphere-ecosystem/managed-services-registry/internal/harbor"
)

var _ = Describe("Client", func() {
	It("should prefix requests with /api/v2.0", func() {
		var gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte("[]"))
		}))
		DeferCleanup(server.Close)

		client, err := harbor.NewClient(harbor.Config{BaseURL: server.URL, AuthMode: harbor.AuthModeBasic})
		Expect(err).NotTo(HaveOccurred())

		_, err = client.ListProjects(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(gotPath).To(Equal("/api/v2.0/projects"))
	})
})
