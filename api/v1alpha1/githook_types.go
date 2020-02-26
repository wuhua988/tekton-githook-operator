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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SecretValueFromSource Secret 来源
type SecretValueFromSource struct {
	// 选择的密钥
	SecretKeyRef *corev1.SecretKeySelector `json:"SecretKeyRef,omitempty"`
}

// +kubebuilder:validation:Enum=gitlab;github;gogs

// GitProvider Git 仓库类型
type GitProvider string

var (
	Gitlab GitProvider = "gitlab"
	Github GitProvider = "github"
	Gogs   GitProvider = "gogs"
)

// +kubebuilder:validation:Enum=create;delete;fork;push;issues;issue_comment;pull_request;release
type gitEvent string

// GitHookSpec defines the desired state of GitHook
type GitHookSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ServiceAccountName K8s 服务账户名称
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ProjectURL Git 项目地址
	// +kubebuilder:validation:MinLength=1
	ProjectURL string `json:"projectUrl"`

	// GitProvider Git 仓库类型
	GitProvider string `json:"gitProvider"`

	// EventTypes 从 Gogs 接收的事件类型
	EventTypes []gitEvent `json:"eventTypes"`

	// AccessToken Gogs 的 access token，保存在 Kubernetes Secret 中
	AccessToken SecretValueFromSource `json:"accessToken"`

	// SecretToken Gogs 的 secret token，保存在 Kubernetes Secret 中
	SecretToken SecretValueFromSource `json:"secretToken"`

	// SSLVerify 触发 hook 时是否执行 SSL 验证
	// +optional
	SSLVerify bool `json:"sslVerify,omitempty"`

	// RunSpec 事件触发时要运行的 tekton pipelinerun spec
	RunSpec tektonv1alpha.PipelineRunSpec `json:"runSpec"`
}

// GitHookStatus defines the observed state of GitHook
type GitHookStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

}

// +kubebuilder:object:root=true

// GitHook is the Schema for the githooks API
type GitHook struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitHookSpec   `json:"spec,omitempty"`
	Status GitHookStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GitHookList contains a list of GitHook
type GitHookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitHook `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitHook{}, &GitHookList{})
}
