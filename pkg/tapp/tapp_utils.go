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

package tapp

import (
	"fmt"

	v1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	client "k8s.io/client-go/kubernetes/typed/core/v1"
)

// overlappingTApps sorts a list of TApps by creation timestamp, using their names as a tie breaker.
// Generally used to tie break between TApps that have overlapping selectors.
type overlappingTApps []*v1.TApp

func (o overlappingTApps) Len() int      { return len(o) }
func (o overlappingTApps) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o overlappingTApps) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// podClient returns the given podClient for the given kubeClient/ns.
func podClient(kubeClient kubernetes.Interface, ns string) client.PodInterface {
	return kubeClient.CoreV1().Pods(ns)
}

// pvcClient returns the given pvcClient for the given kubeClient/ns.
func pvcClient(kubeClient kubernetes.Interface, ns string) client.PersistentVolumeClaimInterface {
	return kubeClient.CoreV1().PersistentVolumeClaims(ns)
}

func getPodTemplate(spec *v1.TAppSpec, id string) (*corev1.PodTemplateSpec, error) {
	templateName := getPodTemplateName(spec.Templates, id, spec.DefaultTemplateName)

	if templateName == v1.DefaultTemplateName {
		return &spec.Template, nil
	} else if template, ok := spec.TemplatePool[templateName]; ok {
		return &template, nil
	} else {
		return nil, fmt.Errorf("template not found in templatePool")
	}
}

func getPodIndex(pod *corev1.Pod) (string, error) {
	if key, ok := pod.Labels[v1.TAppInstanceKey]; ok {
		return key, nil
	}
	return "", fmt.Errorf("pod: %s without %s", pod.Name, v1.TAppInstanceKey)
}

func isPodInActive(pod *corev1.Pod) bool {
	return isPodDying(pod) || pod.Status.Phase != corev1.PodRunning
}

func isPodDying(pod *corev1.Pod) bool {
	return pod != nil && pod.DeletionTimestamp != nil
}

func isPodCompleted(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		return true
	}
	return false
}

func getPodFullName(pod *corev1.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

func isPodFailed(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodFailed {
		return true
	}
	return false
}

func isTAppFinished(tapp *v1.TApp) bool {
	appStatus := tapp.Status.AppStatus
	if appStatus == v1.AppFailed || appStatus == v1.AppSucc || appStatus == v1.AppKilled {
		return true
	}
	return false
}

func isInstanceAlive(status v1.InstanceStatus) bool {
	switch status {
	case v1.InstanceNotCreated:
		return true
	case v1.InstancePending:
		return true
	case v1.InstanceRunning:
		return true
	case v1.InstanceKilling:
		return true
	case v1.InstanceUnknown:
		return true
	case v1.InstancePodFailed:
		return true
	case v1.InstancePodSucc:
		return true
	}
	return false
}

func getRollingTemplateKey(tapp *v1.TApp) string {
	if len(tapp.Spec.UpdateStrategy.Template) != 0 {
		return tapp.Spec.UpdateStrategy.Template
	} else {
		return tapp.Spec.DefaultTemplateName
	}
}

func shouldRollUpdate(tapp *v1.TApp, updates []*Instance) bool {
	template := getRollingTemplateKey(tapp)
	if len(template) == 0 {
		return false
	}

	for _, update := range updates {
		if getPodTemplateName(tapp.Spec.Templates, update.id, tapp.Spec.DefaultTemplateName) == template {
			return true
		}
	}
	return false
}

func extractInstanceId(instances []*Instance) []string {
	a := []string{}
	for _, instance := range instances {
		a = append(a, instance.id)
	}
	return a
}

// getPodTemplateName returns template name for pod whose id is podId, default is 'DefaultTemplateName'
func getPodTemplateName(templates map[string]string, podId string, defaultTemplateName string) string {
	if name, exist := templates[podId]; !exist {
		return defaultTemplateName
	} else {
		return name
	}
}

// GetPodCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func GetPodCondition(status *corev1.PodStatus, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}
