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
	"time"

	"github.com/go-logr/logr"
	servingv1beta1 "github.com/knative/serving/pkg/apis/serving/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	servinv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	toolsv1alpha1 "github.com/zhd173/githook/api/v1alpha1"
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

func (r *GitHookReconciler) sourceLogger(source *toolsv1alpha1.GitHook) logr.Logger {
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
// +kubebuilder:rbac:groups=tools.pongzt.com,resources=githooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tools.pongzt.com,resources=githooks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.knative.dev,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=eventing.knative.dev,resources=channels,verbs=get;list;watch

// Reconcile ...
func (r *GitHookReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.requestLogger(req)

	log.Info("Reconciling" + req.NamespacedName.String())

	// Fetch the GitHook instance
	sourceOrg := &toolsv1alpha1.GitHook{}
	err := r.Get(context.Background(), req.NamespacedName, sourceOrg)
	if err != nil {
		// requeue the request
		return ctrl.Result{}, ignoreNotFound(err)
	}

	source := sourceOrg.DeepCopyObject()

	var reconcileErr error
	if sourceOrg.ObjectMeta.DeletionTimestamp == nil {
		// 新建、更新
		reconcileErr = r.reconcile(source.(*toolsv1alpha1.GitHook))
	} else {
		// 删除：通过 DeletionTimestamp != nil 判定是否删除，调用 finalize 方法删除依赖资源
		if r.hasFinalizer(source.(*toolsv1alpha1.GitHook).Finalizers) {
			reconcileErr = r.finalize(source.(*toolsv1alpha1.GitHook))
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
func (r *GitHookReconciler) reconcile(source *toolsv1alpha1.GitHook) error {
	log := r.sourceLogger(source)
	ksvc, err := r.reconcileWebhookService(source)
	if err != nil {
		return err
	}

	// 使用 Knative Service URL 注册 git webhook，并保存返回的 ID
	return nil
}

// 调和 Knative Service，不存在则创建，否则更新
func (r *GitHookReconciler) reconcileWebhookService(source *toolsv1alpha1.GitHook) (*servinv1alpha1.Service, error) {
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
func (r *GitHookReconciler) generateKnativeServiceObject(source *toolsv1alpha1.GitHook, receiveAdapterImage string) (*servinv1alpha1.Service, error) {
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
func (r *GitHookReconciler) getOwnedKnativeService(source *toolsv1alpha1.GitHook) (*servinv1alpha1.Service, error) {
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

func (r *GitHookReconciler) waitForKnativeServiceReady(source *toolsv1alpha1.GitHook) (*servinv1alpha1.Service, error) {
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

// 删除逻辑
func (r *GitHookReconciler) finalize(*toolsv1alpha1.GitHook) error {
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

func (r *GitHookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&toolsv1alpha1.GitHook{}).
		Complete(r)
}
