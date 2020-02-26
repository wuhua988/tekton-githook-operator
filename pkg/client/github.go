package client

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/go-github/v26/github"
	"github.com/zhd173/githook/pkg/model"
	"golang.org/x/oauth2"
)

// GithubClient provides github git client functionalities
type GithubClient struct {
	authenticatedCtx context.Context
	githubClient     *github.Client
}

// NewGithubClient creates new github git client
func NewGithubClient(accessToken string) *GithubClient {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	githubClient := github.NewClient(tc)

	return &GithubClient{
		authenticatedCtx: ctx,
		githubClient:     githubClient,
	}
}

// Validate checks if hook has been changed
func (client *GithubClient) Validate(options *model.HookOptions) (exists bool, changed bool, err error) {
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

func (client *GithubClient) getHook(options *model.HookOptions) (*github.Hook, error) {
	ID, err := strconv.Atoi(options.ID)

	if err != nil {
		return nil, err
	}
	hook, _, err := client.githubClient.Repositories.GetHook(client.authenticatedCtx, options.Owner, options.Project, int64(ID))

	if err != nil {
		return nil, fmt.Errorf("Failed to list webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	return hook, nil
}

// Create creates webhook
func (client *GithubClient) Create(options *model.HookOptions) (string, error) {
	hookOptions := &github.Hook{
		Config: map[string]interface{}{
			"content_type": "json",
			"url":          options.URL,
			"secret":       options.SecretToken,
		},
		Events: options.Events,
	}

	hook, _, err := client.githubClient.Repositories.CreateHook(client.authenticatedCtx, options.Owner, options.Project, hookOptions)
	if err != nil {
		return "", fmt.Errorf("Failed to add webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	if err != nil {
		return "", fmt.Errorf("fail to create new hook: %s", err)
	}

	return strconv.Itoa(int(*hook.ID)), nil
}

// Update updates webhook
func (client *GithubClient) Update(options *model.HookOptions) (string, error) {
	githubClient := client.githubClient

	if options.ID == "" {
		return "", fmt.Errorf("webhook id is required to be updated")
	}

	hookOptions := &github.Hook{
		Config: map[string]interface{}{
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

	hook, _, err := githubClient.Repositories.EditHook(client.authenticatedCtx, options.Owner, options.Project, int64(hookID), hookOptions)

	if err != nil {
		return "", fmt.Errorf("Failed to update webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	return strconv.Itoa(int(*hook.ID)), err
}

// Delete webhook
func (client *GithubClient) Delete(options *model.HookOptions) error {
	if options.ID != "" {
		hookID, err := strconv.Atoi(options.ID)
		if err != nil {
			return fmt.Errorf("failed to convert hook id to int: " + err.Error())
		}

		_, err = client.githubClient.Repositories.DeleteHook(client.authenticatedCtx, options.Owner, options.Project, int64(hookID))
		if err != nil {
			return fmt.Errorf("failed to delete hook owner '%s' project '%s' : %s", options.Owner, options.Project, err)
		}
	}

	return nil
}
