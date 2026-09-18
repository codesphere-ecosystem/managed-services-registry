// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import "github.com/codesphere-cloud/managed-services-lib/model"

// PlanParameters represents the resources assigned to a Harbor service.
type PlanParameters struct {
	StorageMiB int `json:"storage"`
	CPUTenths  int `json:"cpu"`
	MemoryMiB  int `json:"memory"`
}

// ServiceSecrets contains the credentials used for the managed robot account.
type ServiceSecrets struct {
	SuperuserPassword string `json:"superuserPassword"`
}

// ServiceConfig represents Harbor-specific service options.
type ServiceConfig struct {
	Public bool `json:"public"`
}

// Details represents Harbor-specific runtime details.
type Details struct {
	Ready              bool    `json:"ready"`
	ProjectName        string  `json:"projectName"`
	Username           string  `json:"username"`
	HarborURL          string  `json:"harborUrl"`
	RegistryURL        string  `json:"registryUrl"`
	UsedStorage        int     `json:"usedStorage"`
	UsedStoragePercent float64 `json:"usedStoragePercent"`
}

// Status is the provider status response.
type Status = model.ServiceStatus[PlanParameters, ServiceConfig, Details]

// UpdateArgs is the update payload for the Harbor managed service.
type UpdateArgs struct {
	Config  *ServiceConfig                  `json:"config,omitempty"`
	Plan    *model.PlanSpec[PlanParameters] `json:"plan,omitempty"`
	Secrets *ServiceSecrets                 `json:"secrets,omitempty"`
}
