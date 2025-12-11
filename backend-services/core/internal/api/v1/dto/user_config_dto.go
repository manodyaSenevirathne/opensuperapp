package dto

import "encoding/json"

type UserConfigResponse struct {
	Email       string          `json:"email"`
	ConfigKey   string          `json:"configKey"`
	ConfigValue json.RawMessage `json:"configValue"`
	IsActive    int             `json:"isActive"`
}

type UpsertUserConfigRequest struct {
	ConfigKey   string          `json:"configKey" validate:"required"`
	ConfigValue json.RawMessage `json:"configValue" validate:"required"`
	IsActive    int             `json:"isActive"`
}
