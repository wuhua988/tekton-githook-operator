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

	"github.com/go-logr/logr"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolsv1alpha1 "github.com/zhd173/githook/api/v1alpha1"
)

const (
	controllerAgentName = "githook-controller"
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

func ignoreNotFound(err error) error {
	if apierrs.IsNotFound(err) {
		return nil
	}
	return err
}

// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tools.github.com/zhd173,resources=githooks/status,verbs=get;update;patch

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

	if err := r.Update(context.Background(), source); err != nil {
		log.Error(err, "Failed to update")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, reconcileErr

}

// 新建、更新逻辑
func (r *GitHookReconciler) reconcile(*toolsv1alpha1.GitHook) error {
	// 1. 获取 Knative Service，不存在则创建，否则更新
	// 2. 使用上一步给出的 Knative Service URL 注册 git webhook，并保存返回的 ID
	// 3. 更新 GitHook 资源状态
	return nil
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
