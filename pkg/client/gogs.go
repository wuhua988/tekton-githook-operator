package client

import (
	"fmt"
	"strconv"

	gogs "github.com/gogits/go-gogs-client"
	"github.com/zhd173/githook/pkg/model"
)

// GogsClient provides gogs git client functionalities
type GogsClient struct {
	gogsClient *gogs.Client
}

// NewGogsClient creates new gogs git client
func NewGogsClient(baseURL, accessToken string) *GogsClient {
	gogsClient := gogs.NewClient(baseURL, accessToken)

	return &GogsClient{
		gogsClient,
	}
}

// Validate checks if hook has been changed
func (client *GogsClient) Validate(options *model.HookOptions) (exists bool, changed bool, err error) {
	if options.ID == "" {
		return false, false, nil
	}

	hook, err := client.getHook(options)

	if err != nil {
		return false, false, err
	}

	if hook == nil {
		return false, false, nil
	}

	if hook.Config["url"] != options.URL {
		return true, true, nil
	}

	if len(hook.Events) != len(options.Events) {
		return true, true, nil
	}

	eventSet := make(map[string]bool)

	for _, event := range hook.Events {
		eventSet[event] = true
	}

	for _, event := range options.Events {
		if !eventSet[event] {
			return true, true, nil
		}
	}

	return true, false, nil
}

func (client *GogsClient) getHook(options *model.HookOptions) (*gogs.Hook, error) {
	hooks, err := client.gogsClient.ListRepoHooks(options.Owner, options.Project)

	if err != nil {
		return nil, fmt.Errorf("Failed to list webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	for _, hook := range hooks {
		if strconv.Itoa(int(hook.ID)) == options.ID {
			return hook, nil
		}
	}

	return nil, nil
}

// Create creates webhook
func (client *GogsClient) Create(options *model.HookOptions) (string, error) {
	hookOptions := gogs.CreateHookOption{
		Active: true,
		Config: map[string]string{
			"content_type": "json",
			"url":          options.URL,
			"secret":       options.SecretToken,
		},
		Events: options.Events,
		Type:   "gogs",
	}

	hook, err := client.gogsClient.CreateRepoHook(options.Owner, options.Project, hookOptions)
	if err != nil {
		return "", fmt.Errorf("Failed to add webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	if err != nil {
		return "", fmt.Errorf("fail to create new hook: %s", err)
	}

	return strconv.Itoa(int(hook.ID)), nil
}

// Update updates webhook
func (client *GogsClient) Update(options *model.HookOptions) (string, error) {
	gogsClient := client.gogsClient

	if options.ID == "" {
		return "", fmt.Errorf("webhook id is required to be updated")
	}

	active := true

	hookOptions := gogs.EditHookOption{
		Active: &active,
		Config: map[string]string{
			"content_type": "json",
			"url":          options.URL,
			"secret":       options.SecretToken,
		},
		Events: options.Events,
	}

	hookID, err := strconv.Atoi(options.ID)

	if err != nil {
		return "", fmt.Errorf("cannot convert hook ID %v", hookID)
	}

	err = gogsClient.EditRepoHook(options.Owner, options.Project, int64(hookID), hookOptions)

	if err != nil {
		return "", fmt.Errorf("Failed to update webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	return strconv.Itoa(hookID), err
}

// Delete webhook
func (client *GogsClient) Delete(options *model.HookOptions) error {
	if options.ID != "" {
		hookID, err := strconv.Atoi(options.ID)
		if err != nil {
			return fmt.Errorf("failed to convert hook id to int: " + err.Error())
		}

		err = client.gogsClient.DeleteRepoHook(options.Owner, options.Project, int64(hookID))
		if err != nil {
			return fmt.Errorf("failed to delete hook owner '%s' project '%s' : %s", options.Owner, options.Project, err)
		}
	}

	return nil
}
