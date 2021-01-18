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
	"reflect"
	"testing"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetInPlaceUpdateStateJson(t *testing.T) {
	oldPod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "containerA",
					Image: "nginx",
				},
				{
					Name:  "containerB",
					Image: "2048",
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:    "containerA",
					Image:   "nginx",
					ImageID: "123456",
				},
				{
					Name:    "containerB",
					Image:   "2048",
					ImageID: "12345",
				},
			},
		},
	}
	pod0 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{tappv1.TAppInstanceKey: "0"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "containerA",
					Image: "nginx",
				},
				{
					Name:  "containerB",
					Image: "2048",
				},
			},
		},
	}
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{tappv1.TAppInstanceKey: "1"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "containerA",
					Image: "nginx:1.7.9",
				},
				{
					Name:  "containerB",
					Image: "2048",
				},
			},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{tappv1.TAppInstanceKey: "2"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "containerA",
					Image: "nginx:1.7.9",
				},
				{
					Name:  "containerB",
					Image: "2048:v1",
				},
			},
		},
	}
	Annotation0 := `{"lastContainerStatuses":{}}`
	Annotation1 := `{"lastContainerStatuses":{"containerA":{"imageID":"123456"}}}`
	Annotation2 := `{"lastContainerStatuses":{"containerA":{"imageID":"123456"},"containerB":{"imageID":"12345"}}}`
	pods := []*corev1.Pod{pod0, pod1, pod2}
	expectAnnotations := map[string]string{"0": Annotation0, "1": Annotation1, "2": Annotation2}
	annotations := getAnnotation(oldPod, pods)
	if !reflect.DeepEqual(expectAnnotations, annotations) {
		t.Errorf("Failed to GetInPlaceUpdateStateJson, expected: %+v, got: %+v", expectAnnotations, annotations)
	}
}

// getAnnotation returns a map whose key is pod id(e.g. "0", "1"), the map records the InPlaceUpdateStateJson in Annotation
func getAnnotation(oldPod *corev1.Pod, pods []*corev1.Pod) map[string]string {
	annotations := make(map[string]string)
	for _, pod := range pods {
		if err := getInPlaceUpdateStateJson(pod, oldPod); err == nil {
			if id, err := getPodIndex(pod); err == nil {
				annotations[id] = pod.Annotations[InPlaceUpdateStateKey]
			}
		}
	}
	return annotations
}
