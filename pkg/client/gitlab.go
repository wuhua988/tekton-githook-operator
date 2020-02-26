package client

import (
	"fmt"
	"strconv"

	gitlabclient "github.com/xanzy/go-gitlab"
	"github.com/zhd173/githook/pkg/model"
)

// Event represents gitlab event type
type Event string

const (
	// PushEvents represents push event
	PushEvents Event = "push"

	// IssuesEvents represents issues event
	IssuesEvents Event = "issues"

	// CommentEvents represents issue_comment event
	CommentEvents Event = "issue_comment"

	// MergeRequestEvents represents push pull_request
	MergeRequestEvents Event = "pull_request"
)

// GitlabClient provides gitlab git client functionalities
type GitlabClient struct {
	gitlabClient *gitlabclient.Client
}

func hookToEventList(hook *gitlabclient.ProjectHook) []Event {
	events := make([]Event, 0)

	if hook.PushEvents || hook.TagPushEvents {
		events = append(events, PushEvents)
	}

	if hook.IssuesEvents {
		events = append(events, IssuesEvents)
	}

	if hook.MergeRequestsEvents {
		events = append(events, MergeRequestEvents)
	}

	if hook.NoteEvents {
		events = append(events, CommentEvents)
	}

	return events
}

func eventListToAddHook(events []string, hook *gitlabclient.AddProjectHookOptions) {
	trueValue := true

	for _, event := range events {
		switch Event(event) {
		case PushEvents:
			hook.PushEvents = &trueValue
		case IssuesEvents:
			hook.IssuesEvents = &trueValue
		case MergeRequestEvents:
			hook.MergeRequestsEvents = &trueValue
		case CommentEvents:
			hook.NoteEvents = &trueValue
		}
	}

}

func pid(options *model.HookOptions) string {
	return fmt.Sprintf("%s/%s", options.Owner, options.Project)
}

func eventListToEditHook(events []string, hook *gitlabclient.EditProjectHookOptions) {
	trueValue := true

	for _, event := range events {
		switch Event(event) {
		case PushEvents:
			hook.PushEvents = &trueValue
		case IssuesEvents:
			hook.IssuesEvents = &trueValue
		case MergeRequestEvents:
			hook.MergeRequestsEvents = &trueValue
		case CommentEvents:
			hook.NoteEvents = &trueValue
		}
	}

}

// NewGitlabClient creates new gitlab git client
func NewGitlabClient(baseURL, accessToken string) *GitlabClient {
	gitlabClient := gitlabclient.NewClient(nil, accessToken)
	err := gitlabClient.SetBaseURL(baseURL)
	if err != nil {
		return nil
	}

	return &GitlabClient{
		gitlabClient,
	}
}

// Validate checks if hook has been changed
func (client *GitlabClient) Validate(options *model.HookOptions) (exists bool, changed bool, err error) {
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

	if hook.URL != options.URL {
		return true, true, nil
	}

	events := hookToEventList(hook)
	if len(events) != len(options.Events) {
		return true, true, nil
	}

	eventSet := make(map[string]bool)

	for _, event := range events {
		eventSet[string(event)] = true
	}

	for _, event := range options.Events {
		if !eventSet[event] {
			return true, true, nil
		}
	}

	return true, false, nil
}

func (client *GitlabClient) getHook(options *model.HookOptions) (*gitlabclient.ProjectHook, error) {
	ID, err := strconv.Atoi(options.ID)

	if err != nil {
		return nil, err
	}

	hook, _, err := client.gitlabClient.Projects.GetProjectHook(pid(options), ID)

	if err != nil {
		return nil, fmt.Errorf("Failed to list webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	return hook, nil
}

// Create creates webhook
func (client *GitlabClient) Create(options *model.HookOptions) (string, error) {
	hookOptions := &gitlabclient.AddProjectHookOptions{
		URL:   &options.URL,
		Token: &options.SecretToken,
	}

	eventListToAddHook(options.Events, hookOptions)

	hook, _, err := client.gitlabClient.Projects.AddProjectHook(pid(options), hookOptions)
	if err != nil {
		return "", fmt.Errorf("Failed to add webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	if err != nil {
		return "", fmt.Errorf("fail to create new hook: %s", err)
	}

	return strconv.Itoa(int(hook.ID)), nil
}

// Update updates webhook
func (client *GitlabClient) Update(options *model.HookOptions) (string, error) {
	gitlabClient := client.gitlabClient

	if options.ID == "" {
		return "", fmt.Errorf("webhook id is required to be updated")
	}

	hookOptions := &gitlabclient.EditProjectHookOptions{
		URL:   &options.URL,
		Token: &options.SecretToken,
	}

	eventListToEditHook(options.Events, hookOptions)

	hookID, err := strconv.Atoi(options.ID)

	if err != nil {
		return "", fmt.Errorf("cannot convert hook ID %v", hookID)
	}

	hook, _, err := gitlabClient.Projects.EditProjectHook(pid(options), hookID, hookOptions)

	if err != nil {
		return "", fmt.Errorf("Failed to update webhook to the Project:" + options.Project + " due to " + err.Error())
	}

	return strconv.Itoa(hook.ID), err
}

// Delete webhook
func (client *GitlabClient) Delete(options *model.HookOptions) error {
	if options.ID != "" {
		hookID, err := strconv.Atoi(options.ID)
		if err != nil {
			return fmt.Errorf("failed to convert hook id to int: " + err.Error())
		}

		_, err = client.gitlabClient.Projects.DeleteProjectHook(pid(options), hookID)
		if err != nil {
			return fmt.Errorf("failed to delete hook owner '%s' project '%s' : %s", options.Owner, options.Project, err)
		}
	}

	return nil
}
