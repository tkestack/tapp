/*
 * Tencent is pleased to support the open source community by making TKEStack available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	DefaultTemplateName = "default"
	TAppInstanceKey     = "tapp_instance_key"

	// If its value is false, it means pod is being updated.
	InPlaceUpdateReady corev1.PodConditionType = "tkestack.io/InPlaceUpdate"

	// DefaultMaxUnavailable is the default value for .Spec.UpdateStrategy.MaxUnavailable
	DefaultMaxUnavailable = 1

	// DefaultMaxUnavailable is the default value for .Spec.UpdateStrategy.ForceUpdateStrategy.MaxUnavailable
	DefaultMaxUnavailableForceUpdate = "100%"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TApp represents a set of pods with consistent identities.
type TApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired identities of pods in this tapp.
	Spec TAppSpec `json:"spec,omitempty"`

	// Status is the current status of pods in this TApp. This data
	// may be out of date by some window of time.
	Status TAppStatus `json:"status,omitempty"`
}

// A TAppSpec is the specification of a TApp.
type TAppSpec struct {
	// Replicas is the desired number of replicas of the given Template.
	// These are replicas in the sense that they are instantiations of the
	// same Template, but individual replicas also have a consistent identity.
	Replicas int32 `json:"replicas"`

	// Selector is a label query over pods that should match the replica count.
	// If empty, defaulted to labels on the pod template.
	// More info: http://releases.k8s.io/release-1.4/docs/user-guide/labels.md#label-selectors
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Template is the object that describes the pod that will be initial created/default scaled
	// it should be added to TemplatePool
	Template corev1.PodTemplateSpec `json:"template"`

	// TemplatePool stores a map whose key is template name and value is podTemplate
	TemplatePool map[string]corev1.PodTemplateSpec `json:"templatePool,omitempty"`

	// Statuses stores desired instance status instanceID --> desiredStatus
	Statuses map[string]InstanceStatus `json:"statuses,omitempty"`

	// Templates stores instanceID --> template name
	Templates map[string]string `json:"templates,omitempty"`

	// UpdateStrategy indicates the TappUpdateStrategy that will be
	// employed to update Pods in the TApp
	UpdateStrategy TAppUpdateStrategy `json:"updateStrategy,omitempty"`

	// ForceDeletePod indicates whether force delete pods when it is being deleted because of NodeLost.
	// Default values is false.
	ForceDeletePod bool `json:"forceDeletePod,omitempty"`

	// AutoDeleteUnusedTemplate indicates whether auto delete templates when it is unused.
	// Default values is false.
	AutoDeleteUnusedTemplate bool `json:"autoDeleteUnusedTemplate,omitempty"`

	// NeverMigrate indicates whether to migrate pods. If it is true, pods will never be migrated to
	// other nodes, otherwise it depends on other conditions(e.g. pod restart policy).
	NeverMigrate bool `json:"neverMigrate,omitempty"`

	// volumeClaimTemplates is a list of claims that pods are allowed to reference.
	// The TApp controller is responsible for mapping network identities to
	// claims in a way that maintains the identity of a pod. Every claim in
	// this list must have at least one matching (by name) volumeMount in one
	// container in the template. A claim in this list takes precedence over
	// any volumes in the template, with the same name.
	// TODO: Define the behavior if a claim already exists with the same name.
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

	// ServiceName is the name of the service that governs this TApp.
	// This service must exist before the TApp, and is responsible for
	// the network identity of the set. Pods get DNS/hostnames that follow the
	// pattern: pod-specific-string.serviceName.default.svc.cluster.local
	// where "pod-specific-string" is managed by the TApp controller.
	ServiceName string `json:"serviceName,omitempty"`

	//DefaultTemplateName is the default template name for scale
	DefaultTemplateName string `json:"defaultTemplateName"`
}

// TApp update strategy
type TAppUpdateStrategy struct {
	// Following fields are rolling update related configuration.
	// Template is the rolling update template name
	Template string `json:"template,omitempty"`
	// MaxUnavailable is the max unavailable number when tapp is rolling update, default is 1.
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// Following fields are force update related configuration.
	ForceUpdate ForceUpdateStrategy `json:"forceUpdate,omitempty"`
}

type ForceUpdateStrategy struct {
	// MaxUnavailable is the max unavailable number when tapp is forced update, default is 100%.
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

type InstanceStatus string

const (
	InstanceNotCreated InstanceStatus = "NotCreated"
	InstancePending    InstanceStatus = "Pending"
	InstanceRunning    InstanceStatus = "Running"
	InstanceUpdating   InstanceStatus = "Updating"
	InstancePodFailed  InstanceStatus = "PodFailed"
	InstancePodSucc    InstanceStatus = "PodSucc"
	InstanceKilling    InstanceStatus = "Killing"
	InstanceKilled     InstanceStatus = "Killed"
	InstanceFailed     InstanceStatus = "Failed"
	InstanceSucc       InstanceStatus = "Succ"
	InstanceUnknown    InstanceStatus = "Unknown"
)

var InstanceStatusAll = []InstanceStatus{InstanceNotCreated, InstancePending, InstanceRunning, InstanceUpdating,
	InstanceFailed, InstanceKilling, InstanceSucc, InstanceKilled}

type AppStatus string

const (
	AppPending AppStatus = "Pending"
	AppRunning AppStatus = "Running"
	AppFailed  AppStatus = "Failed"
	AppSucc    AppStatus = "Succ"
	AppKilled  AppStatus = "Killed"
)

// TAppStatus represents the current state of a TApp.
type TAppStatus struct {
	// most recent generation observed by controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Replicas is the number of actual replicas.
	Replicas int32 `json:"replicas"`

	// ReadyReplicas is the number of running replicas
	ReadyReplicas int32 `json:"readyReplicas"`

	// ScaleSelector is a label for query over pods that should match the replica count used by HPA.
	ScaleLabelSelector string `json:"scaleLabelSelector,omitempty"`

	// AppStatus describe the current TApp state
	AppStatus AppStatus `json:"appStatus,omitempty"`

	// Statues stores actual instanceID --> InstanceStatus
	Statuses map[string]InstanceStatus `json:"statuses,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TAppList is a collection of TApp.
type TAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TApp `json:"items"`
}
