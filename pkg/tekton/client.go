package tekton

import (
	"encoding/json"
	"fmt"

	v1alpha1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Client provides tekton client
type Client struct {
	tekton *versioned.Clientset
}

// PipelineOptions stores pipeline options
type PipelineOptions struct {
	Namespace   string
	Prefix      string
	GitURL      string
	GitRevision string
	GitCommit   string
	RunSpecJSON string
}

// New creates new tekton client instance
func New() (*Client, error) {
	config := ctrl.GetConfigOrDie()

	clientset, err := versioned.NewForConfig(config)

	if err != nil {
		return nil, err
	}

	return &Client{
		tekton: clientset,
	}, nil
}

func (client *Client) getOrCreateGitPipelineResource(namespace, prefix, gitURL, revision string) (string, error) {

	tektonClient := client.tekton.TektonV1alpha1()
	// search for existing resource
	list, err := tektonClient.PipelineResources(namespace).List(metav1.ListOptions{})

	if err != nil {
		return "", fmt.Errorf("failed to list pipeline resource: %s", err)
	}

	for _, item := range list.Items {
		if item.Spec.Type == v1alpha1.PipelineResourceTypeGit {
			gitResource, _ := v1alpha1.NewGitResource(&item)
			if gitResource.URL == gitURL {
				return item.Name, nil
			}
		}
	}

	// create new
	gitResource := &v1alpha1.PipelineResource{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-git-source-", prefix),
			Namespace:    namespace,
		},
		Spec: v1alpha1.PipelineResourceSpec{
			Type: v1alpha1.PipelineResourceTypeGit,
			Params: []v1alpha1.Param{
				v1alpha1.Param{
					Name:  "url",
					Value: gitURL,
				},
				v1alpha1.Param{
					Name:  "rivision",
					Value: revision,
				},
			},
		},
	}

	gitResource, err = tektonClient.PipelineResources(namespace).Create(gitResource)

	if err != nil {
		return "", fmt.Errorf("failed to create pipeline resource: %s", err)
	}

	return gitResource.Name, nil
}

// CreatePipelineRun creates new pipeline run
func (client *Client) CreatePipelineRun(options PipelineOptions) (*v1alpha1.PipelineRun, error) {
	return client.generatePipelineRun(options)
}

func (client *Client) generatePipelineRun(options PipelineOptions) (*v1alpha1.PipelineRun, error) {

	pipelineRunSpec := &v1alpha1.PipelineRunSpec{}
	err := json.Unmarshal([]byte(replaceVars(options.RunSpecJSON, options)), pipelineRunSpec)

	if err != nil {
		return nil, err
	}

	pipelineRun := &v1alpha1.PipelineRun{}
	pipelineRun.Spec = *pipelineRunSpec
	pipelineRun.ObjectMeta = metav1.ObjectMeta{
		GenerateName: fmt.Sprintf("%s-", options.Prefix),
		Namespace:    options.Namespace,
	}

	if len(pipelineRun.Spec.Resources) == 0 {
		gitResourceName, err := client.getOrCreateGitPipelineResource(options.Namespace, options.Prefix, options.GitURL, options.GitRevision)

		if err != nil {
			return nil, err
		}

		pipelineRun.Spec.Resources = []v1alpha1.PipelineResourceBinding{
			v1alpha1.PipelineResourceBinding{
				Name: "git-source",
				ResourceRef: v1alpha1.PipelineResourceRef{
					Name: gitResourceName,
				},
			},
		}
	}

	tektonClient := client.tekton.TektonV1alpha1()

	pipelineRun, err = tektonClient.PipelineRuns(options.Namespace).Create(pipelineRun)

	if err != nil {
		return nil, fmt.Errorf("error creating pipeline run: %s", err)
	}

	return pipelineRun, nil
}
