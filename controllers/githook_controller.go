/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	servinv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servingv1beta1 "github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/zhd173/githook/api/v1alpha1"
	githookclient "github.com/zhd173/githook/pkg/client"
	"github.com/zhd173/githook/pkg/githook"
	"github.com/zhd173/githook/pkg/model"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	controllerAgentName = "githook-controller"
	runKsvcAs           = "pipeline-runner" // see tektonrole.yaml
	finalizerName       = controllerAgentName
)

// GitHookReconciler reconciles a GitHook object
type GitHookReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	WebhookImage string
}

func (r *GitHookReconciler) requestLogger(req ctrl.Request) logr.Logger {
	return r.Log.WithName(req.NamespacedName.String())
}

func (r *GitHookReconciler) sourceLogger(source *v1alpha1.GitHook) logr.Logger {
	return r.Log.WithName(fmt.Sprintf("%s/%s", source.Namespace, source.Name))
}

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.knative.dev,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=eventing.knative.dev,resources=channels,verbs=get;list;watch

// Reconcile ...
func (r *GitHookReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.requestLogger(req)

	log.Info("Reconciling" + req.NamespacedName.String())

	// Fetch the GitHook instance
	sourceOrg := &v1alpha1.GitHook{}
	err := r.Get(context.Background(), req.NamespacedName, sourceOrg)
	if err != nil {
		// requeue the request
		return ctrl.Result{}, ignoreNotFound(err)
	}

	source := sourceOrg.DeepCopyObject()

	var reconcileErr error
	if sourceOrg.ObjectMeta.DeletionTimestamp == nil {
		// 新建、更新
		reconcileErr = r.reconcile(source.(*v1alpha1.GitHook))
	} else {
		// 删除：通过 DeletionTimestamp != nil 判定是否删除，调用 finalize 方法删除依赖资源
		if r.hasFinalizer(source.(*v1alpha1.GitHook).Finalizers) {
			reconcileErr = r.finalize(source.(*v1alpha1.GitHook))
		}
	}

	// 更新 GitHook 资源状态
	if err := r.Update(context.Background(), source); err != nil {
		log.Error(err, "Failed to update")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, reconcileErr

}

// 新建、更新逻辑
func (r *GitHookReconciler) reconcile(source *v1alpha1.GitHook) error {
	log := r.sourceLogger(source)

	ksvc, err := r.reconcileWebhookService(source)
	if err != nil {
		return err
	}

	hookOptions, err := r.buildHookFromSource(source)
	if err != nil {
		return err
	}

	// 使用 Knative Service URL 注册 git webhook，并保存返回的 ID
	hookOptions.URL = getWebhookURL(source, ksvc)
	hookID, err := r.reconcileWebhook(source, hookOptions)
	if err != nil {
		return err
	}
	source.Status.ID = hookID

	log.Info("add finalizer to the source")
	r.addFinalizer(source)
	return nil
}

// 注册 git webhook
func (r *GitHookReconciler) reconcileWebhook(source *v1alpha1.GitHook, hookOptions *model.HookOptions) (string, error) {
	log := r.sourceLogger(source)

	gitClient, err := getGitClient(source, hookOptions)

	if err != nil {
		return "", err
	}

	exists, changed, err := gitClient.Validate(hookOptions)

	if err != nil {
		return "", err
	}

	if !exists {
		log.Info("create new webhook", "project", hookOptions.Project)
		hookID, err := gitClient.Create(hookOptions)

		if err != nil {
			return "", err
		}
		log.Info("create new webhook successfully", "project", hookOptions.Project)
		return hookID, err
	}

	if err != nil {
		return "", err
	}

	if changed == true {
		log.Info("update existing webhook", "project", hookOptions.Project)
		hookID, err := gitClient.Update(hookOptions)

		if err != nil {
			return "", err
		}

		log.Info("update existing webhook successfully", "project", hookOptions.Project)

		return hookID, nil
	}

	log.Info("webhook exists and updated", "project", hookOptions.Project)
	return hookOptions.ID, nil
}

// 调和 Knative Service，不存在则创建，否则更新
func (r *GitHookReconciler) reconcileWebhookService(source *v1alpha1.GitHook) (*servinv1alpha1.Service, error) {
	log := r.sourceLogger(source)

	desiredKsvc, err := r.generateKnativeServiceObject(source, r.WebhookImage)
	if err != nil {
		return nil, err
	}

	ksvc, err := r.getOwnedKnativeService(source)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return nil, fmt.Errorf("Failed to verify if knative service is created for the gogssource: " + err.Error())
		}

		// no webhook service found, create new
		log.Info("webhook service not exist. create new one.")
		if err = r.Create(context.TODO(), desiredKsvc); err != nil {
			return nil, err
		}
		ksvc = desiredKsvc
		log.Info("webhook service created successfully", "name", ksvc.Name)
	}

	// should update
	if ksvc != desiredKsvc {

		templateUpdated := !apiequality.Semantic.DeepEqual(
			desiredKsvc.Spec.ConfigurationSpec.Template.Spec.PodSpec,
			ksvc.Spec.ConfigurationSpec.Template.Spec.PodSpec)

		if templateUpdated == true {
			log.Info("webhook service template update")
			desiredKsvc.Spec.ConfigurationSpec.Template.Spec.PodSpec.DeepCopyInto(&ksvc.Spec.ConfigurationSpec.Template.Spec.PodSpec)

			if err = r.Update(context.TODO(), ksvc); err != nil {
				return nil, err
			}
			log.Info("webhook service template update successfully")
		}
	}

	log.Info("ensure webhook service is ready", "ksvc name", ksvc.Name)
	ksvc, err = r.waitForKnativeServiceReady(source)
	if err != nil {
		return nil, err
	}
	log.Info("webhook service is ready", "ksvc name", ksvc.Name)

	return ksvc, err
}

// 生成期望 Knative Service 对象
func (r *GitHookReconciler) generateKnativeServiceObject(source *v1alpha1.GitHook, receiveAdapterImage string) (*servinv1alpha1.Service, error) {
	labels := map[string]string{
		"receive-adapter": source.Name,
	}
	env := []corev1.EnvVar{
		{
			Name: "SECRET_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: source.Spec.SecretToken.SecretKeyRef,
			},
		},
	}

	runSpecJSON, err := json.Marshal(source.Spec.RunSpec)
	if err != nil {
		return nil, err
	}

	containerArgs := []string{
		fmt.Sprintf("--gitprovider=%s", source.Spec.GitProvider),
		fmt.Sprintf("--namespace=%s", source.Namespace),
		fmt.Sprintf("--name=%s", source.Name),
		fmt.Sprintf("--runSpecJSON=%s", string(runSpecJSON)),
	}

	ksvc := &servinv1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-webhook-", source.Name),
			Namespace:    source.Namespace,
			Labels:       labels,
		},
		Spec: servinv1alpha1.ServiceSpec{
			ConfigurationSpec: servinv1alpha1.ConfigurationSpec{
				Template: &servinv1alpha1.RevisionTemplateSpec{
					Spec: servinv1alpha1.RevisionSpec{
						RevisionSpec: servingv1beta1.RevisionSpec{
							PodSpec: servingv1beta1.PodSpec{
								ServiceAccountName: runKsvcAs,
								Containers: []corev1.Container{corev1.Container{
									Image: receiveAdapterImage,
									Env:   env,
									Args:  containerArgs,
								}},
							},
						},
					},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(source, ksvc, r.Scheme); err != nil {
		return nil, err
	}
	return ksvc, nil
}

var (
	jobOwnerKey = ".metadata.controller"
)

// 获取旧的 Knative Service
func (r *GitHookReconciler) getOwnedKnativeService(source *v1alpha1.GitHook) (*servinv1alpha1.Service, error) {
	ctx := context.Background()

	list := &servinv1alpha1.ServiceList{}
	if err := r.List(ctx, list, client.InNamespace(source.Namespace), client.MatchingField(jobOwnerKey, source.Name)); err != nil {
		return nil, fmt.Errorf("unable to list knative service %s", err)
	}

	if len(list.Items) <= 0 {
		return nil, apierrs.NewNotFound(servinv1alpha1.Resource("ksvc"), "")
	}

	return &list.Items[0], nil
}

func (r *GitHookReconciler) waitForKnativeServiceReady(source *v1alpha1.GitHook) (*servinv1alpha1.Service, error) {
	for attempts := 0; attempts < 4; attempts++ {
		ksvc, err := r.getOwnedKnativeService(source)
		if err != nil {
			return nil, err
		}
		routeCondition := ksvc.Status.GetCondition(servinv1alpha1.ServiceConditionRoutesReady)
		receiveAdapterAddr := ksvc.Status.Address
		if routeCondition != nil && routeCondition.Status == corev1.ConditionTrue && receiveAdapterAddr != nil {
			return ksvc, nil
		}
		time.Sleep(2000 * time.Millisecond)
	}
	return nil, fmt.Errorf("Failed to get service to be in ready state")
}

func (r *GitHookReconciler) buildHookFromSource(source *v1alpha1.GitHook) (*model.HookOptions, error) {
	hookOptions := &model.HookOptions{}

	baseURL, owner, projectName, err := parseGitURL(source.Spec.ProjectURL)
	if err != nil {
		return nil, fmt.Errorf("failed to process project url to get the project name: " + err.Error())
	}

	hookOptions.BaseURL = baseURL
	hookOptions.Project = projectName
	hookOptions.Owner = owner
	hookOptions.ID = source.Status.ID

	for _, event := range source.Spec.EventTypes {
		hookOptions.Events = append(hookOptions.Events, string(event))
	}
	hookOptions.AccessToken, err = r.secretFrom(source.Namespace, source.Spec.AccessToken.SecretKeyRef)

	if err != nil {
		return nil, fmt.Errorf("failed to get accesstoken from secret %s/%s", source.Namespace, source.Spec.AccessToken.SecretKeyRef.Key)
	}

	hookOptions.SecretToken, err = r.secretFrom(source.Namespace, source.Spec.SecretToken.SecretKeyRef)

	if err != nil {
		return nil, fmt.Errorf("failed to get secret token from secret %s/%s", source.Namespace, source.Spec.AccessToken.SecretKeyRef.Key)
	}

	return hookOptions, nil
}

func parseGitURL(gitURL string) (baseURL string, owner string, project string, err error) {
	u, err := url.Parse(gitURL)
	if err != nil {
		return "", "", "", err
	}

	paths := strings.Split(u.Path[1:], "/")
	baseURL = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	owner = paths[0]
	project = paths[1]

	return baseURL, owner, project, nil
}

func (r *GitHookReconciler) getSecret(namespace string, secretKeySelector *corev1.SecretKeySelector) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := r.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: secretKeySelector.Name}, secret)

	return secret, err
}

func (r *GitHookReconciler) secretFrom(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error) {
	secret, err := r.getSecret(namespace, secretKeySelector)

	if err != nil {
		return "", err
	}
	secretVal, ok := secret.Data[secretKeySelector.Key]
	if !ok {
		return "", fmt.Errorf(`key "%s" not found in secret "%s"`, secretKeySelector.Key, secretKeySelector.Name)
	}

	return string(secretVal), nil
}

func (r *GitHookReconciler) addFinalizer(source *v1alpha1.GitHook) {
	source.Finalizers = insertFinalizer(source.Finalizers)
}

func insertFinalizer(finalizers []string) []string {
	set := sets.NewString(finalizers...)
	set.Insert(finalizerName)
	return set.List()
}

func getGitClient(source *v1alpha1.GitHook, options *model.HookOptions) (*githook.Client, error) {
	var gitClient githook.GitClient

	switch source.Spec.GitProvider {
	case string(v1alpha1.Gogs):
		gitClient = githookclient.NewGogsClient(options.BaseURL, options.AccessToken)
	case string(v1alpha1.Github):
		gitClient = githookclient.NewGithubClient(options.AccessToken)
	case string(v1alpha1.Gitlab):
		gitClient = githookclient.NewGitlabClient(options.BaseURL, options.AccessToken)
	default:
		return nil, fmt.Errorf("git provider %s not support", source.Spec.GitProvider)
	}

	return githook.New(gitClient, options.BaseURL, options.AccessToken)
}

// 删除逻辑
func (r *GitHookReconciler) finalize(*v1alpha1.GitHook) error {
	return nil
}

func (r *GitHookReconciler) hasFinalizer(finalizers []string) bool {
	for _, finalizerStr := range finalizers {
		if finalizerStr == finalizerName {
			return true
		}
	}
	return false
}

// SetupWithManager setups controller with manager
func (r *GitHookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&servinv1alpha1.Service{}, jobOwnerKey, func(rawObj runtime.Object) []string {
		// grab the service object, extract the owner...
		service := rawObj.(*servinv1alpha1.Service)
		owner := metav1.GetControllerOf(service)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.Kind != "GitHook" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GitHook{}).
		Owns(&servinv1alpha1.Service{}).
		Complete(r)
}
