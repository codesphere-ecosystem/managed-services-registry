// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor_test

import (
	"context"
	"log/slog"

	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/codesphere-cloud/managed-services-lib/model"

	"github.com/codesphere-ecosystem/managed-services-registry/internal/harbor"
)

// mockClient embeds harbor.Client so calls without a mocked method panic.
type mockClient struct {
	harbor.Client
	mock.Mock
}

func (m *mockClient) ListProjects(ctx context.Context) ([]*harbormodels.Project, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*harbormodels.Project), args.Error(1)
}

func (m *mockClient) GetProject(ctx context.Context, name string) (*harbormodels.Project, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(*harbormodels.Project), args.Error(1)
}

func (m *mockClient) ListProjectRobots(ctx context.Context, projectID int64) ([]*harbormodels.Robot, error) {
	args := m.Called(ctx, projectID)
	return args.Get(0).([]*harbormodels.Robot), args.Error(1)
}

func (m *mockClient) DeleteProject(ctx context.Context, name string) error {
	return m.Called(ctx, name).Error(0)
}

var _ = Describe("Provider", func() {
	var (
		client   *mockClient
		provider *harbor.Provider
		ctx      context.Context
	)

	BeforeEach(func() {
		client = new(mockClient)
		provider = harbor.NewProvider(harbor.Config{ProjectPrefix: "ms-"}, client, slog.Default())
		ctx = context.Background()
	})

	AfterEach(func() {
		client.AssertExpectations(GinkgoT())
	})

	Describe("List", func() {
		It("should return service IDs without the project prefix", func() {
			client.On("ListProjects", ctx).
				Return([]*harbormodels.Project{{Name: "ms-a"}, {Name: "library"}}, nil)

			ids, err := provider.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(Equal([]model.ServiceID{"a"}))
		})
	})

	Describe("Delete", func() {
		It("should delete the project when its robot account is missing", func() {
			client.On("GetProject", ctx, "ms-a").
				Return(&harbormodels.Project{Name: "ms-a", ProjectID: 1}, nil)
			client.On("ListProjectRobots", ctx, int64(1)).
				Return([]*harbormodels.Robot{}, nil)
			client.On("DeleteProject", ctx, "ms-a").Return(nil)

			Expect(provider.Delete(ctx, "a")).To(Succeed())
		})
	})
})
