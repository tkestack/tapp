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

package hash

import (
	corev1 "k8s.io/api/core/v1"
)

// TappHashInterface is used for generate and verify hash for tapp.
type TappHashInterface interface {
	// SetTemplateHash sets PodTemplateSpec's hash value into template's labels,
	// returns true if needs set and is set, otherwise false
	SetTemplateHash(template *corev1.PodTemplateSpec) bool
	// GetTemplateHash returns PodTemplateSpec's hash value, the values is stored in labels.
	GetTemplateHash(labels map[string]string) string
	// SetUniqHash sets hash value of PodTemplateSpec(without container images) into template's labels,
	// returns true if needs set and is set, otherwise false
	SetUniqHash(template *corev1.PodTemplateSpec) bool
	// GetUniqHash returns hash value of PodTemplateSpec(without container images), the values is stored in labels.
	GetUniqHash(labels map[string]string) string
	// HashLabels returns labels key that stores TemplateHash and UniqHash
	HashLabels() []string
}
