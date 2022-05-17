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

package v2

import (
	"encoding/json"
	"fmt"
	"hash/fnv"

	"tkestack.io/tapp/pkg/hash"

	corev1 "k8s.io/api/core/v1"
	hashutil "k8s.io/kubernetes/pkg/util/hash"
)

const (
	// TemplateHashKeyV2 is a key for storing PodTemplateSpec's hash value in labels.
	// It will be used to check whether pod's PodTemplateSpec has changed, if yes,
	// we need recreate or do in-place update for the pod according to the value of UniqHash.
	TemplateHashKeyV2 = "tapp_template_hash_key_v2"
	// UniqHashKeyV2 is a key for storing hash value of PodTemplateSpec(without container images) in labels.
	// It will will be used to check whether pod's PodTemplateSpec hash changed and only container images
	// changed, if yes, we will do in place update for the pod.
	UniqHashKeyV2 = "tapp_uniq_hash_key_v2"
)

func NewTappHash() hash.TappHashInterface {
	return &defaultTappHashV2{}
}

type defaultTappHashV2 struct{}

func (th *defaultTappHashV2) SetTemplateHash(template *corev1.PodTemplateSpec) bool {
	expected := generateTemplateHash(template)
	h := th.GetTemplateHash(template.Labels)
	if h != expected {
		if template.Labels == nil {
			template.Labels = make(map[string]string)
		}
		template.Labels[TemplateHashKeyV2] = expected
		return true
	} else {
		return false
	}
}

func (th *defaultTappHashV2) GetTemplateHash(labels map[string]string) string {
	return labels[TemplateHashKeyV2]
}

func (th *defaultTappHashV2) SetUniqHash(template *corev1.PodTemplateSpec) bool {
	expected := generateUniqHash(*template)
	h := th.GetUniqHash(template.Labels)
	if h != expected {
		if template.Labels == nil {
			template.Labels = make(map[string]string)
		}
		template.Labels[UniqHashKeyV2] = expected
		return true
	} else {
		return false
	}
}

func (th *defaultTappHashV2) GetUniqHash(labels map[string]string) string {
	return labels[UniqHashKeyV2]
}

func (th *defaultTappHashV2) HashLabels() []string {
	return []string{TemplateHashKeyV2, UniqHashKeyV2}
}

func generateHash(template interface{}) uint64 {
	// Omit nil or empty field when calculating hash value
	// Please see https://github.com/kubernetes/kubernetes/issues/53644
	content, _ := json.Marshal(template)
	hasher := fnv.New64()
	hashutil.DeepHashObject(hasher, content)
	return hasher.Sum64()
}

func generateTemplateHash(template *corev1.PodTemplateSpec) string {
	meta := template.ObjectMeta.DeepCopy()
	delete(meta.Labels, TemplateHashKeyV2)
	delete(meta.Labels, UniqHashKeyV2)
	return fmt.Sprintf("%d", generateHash(corev1.PodTemplateSpec{
		ObjectMeta: *meta,
		Spec:       template.Spec,
	}))
}

func generateUniqHash(template corev1.PodTemplateSpec) string {
	if template.Spec.InitContainers != nil {
		var newContainers []corev1.Container
		for _, container := range template.Spec.InitContainers {
			container.Image = ""
			newContainers = append(newContainers, container)
		}
		template.Spec.InitContainers = newContainers
	}

	var newContainers []corev1.Container
	for _, container := range template.Spec.Containers {
		container.Image = ""
		newContainers = append(newContainers, container)
	}
	template.Spec.Containers = newContainers

	meta := template.ObjectMeta.DeepCopy()
	delete(meta.Labels, TemplateHashKeyV2)
	delete(meta.Labels, UniqHashKeyV2)
	return fmt.Sprintf("%d", generateHash(corev1.PodTemplateSpec{
		ObjectMeta: *meta,
		Spec:       template.Spec,
	}))
}
