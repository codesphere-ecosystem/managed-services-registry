package harbor

import "github.com/codesphere-cloud/managed-services-lib/model"

// ServiceConfig represents Harbor-specific service options.
type ServiceConfig struct {
	Public bool `json:"public"`
}

// Details represents Harbor-specific runtime details.
type Details struct {
	model.ServiceDetails
	ProjectName        string  `json:"projectName"`
	Username           string  `json:"username"`
	HarborURL          string  `json:"harborUrl"`
	RegistryURL        string  `json:"registryUrl"`
	UsedStorage        int     `json:"usedStorage"`
	UsedStoragePercent float64 `json:"usedStoragePercent"`
}

// Service is the create payload for the Harbor managed service.
type Service struct {
	ID      model.ServiceID      `json:"id"`
	TeamID  int                  `json:"teamId,omitempty"`
	Config  ServiceConfig        `json:"config"`
	Plan    model.Plan           `json:"plan"`
	Secrets model.ServiceSecrets `json:"secrets"`
}

// GetID implements model.ManagedService.
func (s Service) GetID() model.ServiceID { return s.ID }

// GetConfig implements model.ManagedService.
func (s Service) GetConfig() model.ServiceConfig { return model.ServiceConfig{} }

// GetPlan implements model.ManagedService.
func (s Service) GetPlan() model.Plan { return s.Plan }

// GetSecrets implements model.ManagedService.
func (s Service) GetSecrets() model.ServiceSecrets { return s.Secrets }

// Status is the provider status response.
type Status struct {
	Config  ServiceConfig `json:"config"`
	Details Details       `json:"details"`
	Error   string        `json:"error,omitempty"`
	Plan    model.Plan    `json:"plan"`
	Pause   bool          `json:"pause"`
}

// UpdateArgs is the update payload for the Harbor managed service.
type UpdateArgs struct {
	Config  *ServiceConfig        `json:"config,omitempty"`
	Plan    *model.Plan           `json:"plan,omitempty"`
	Secrets *model.ServiceSecrets `json:"secrets,omitempty"`
}
