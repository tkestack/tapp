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
	"testing"

	"tkestack.io/tapp/pkg/hash"
	"tkestack.io/tapp/pkg/testutil"

	corev1 "k8s.io/api/core/v1"
)

func TestMergePod(t *testing.T) {
	tapp := testutil.CreateValidTApp(1)
	instance, err := newInstance(tapp, "0")
	if err != nil {
		t.Errorf("new instance failed,error:%+v", err)
	}

	pod := instance.pod

	newPod := pod.DeepCopy()
	newPod.Spec.Containers[0].Image = "image.new"
	newPod.Spec.Containers[0].Name = "name.new"
	newPod.Labels[hash.TemplateHashKey] = "tappHashKey.new"
	mergePod(pod, newPod)
	if pod.Spec.Containers[0].Image != newPod.Spec.Containers[0].Image {
		t.Errorf("pod image not updated")
	}
	if pod.Spec.Containers[0].Name == newPod.Spec.Containers[0].Name {
		t.Errorf("pod name updated")
	}
	if pod.Labels[hash.TemplateHashKey] != newPod.Labels[hash.TemplateHashKey] {
		t.Errorf("TAppHashKey not updated")
	}

	// Check the case that new pod has added some containers
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Image: "test"})
	mergePod(pod, newPod)
	if len(pod.Spec.Containers) != 2 {
		t.Errorf("Expected containers number is 2, got: %d", len(pod.Spec.Containers))
	}
}
