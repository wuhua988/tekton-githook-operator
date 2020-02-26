package model

// HookOptions keeps webhook options
type HookOptions struct {
	AccessToken string
	SecretToken string
	Project     string
	ID          string
	BaseURL     string
	URL         string
	Owner       string
	Events      []string
}
