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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
)

const (
	// InPlaceUpdateStateKey records the state of inplace-update.
	// The value of annotation is InPlaceUpdateState.
	InPlaceUpdateStateKey string = "inplace-update-state"
)

type InPlaceUpdateState struct {
	// LastContainerStatuses records the before-in-place-update container statuses. It is a map from ContainerName
	// to InPlaceUpdateContainerStatus
	LastContainerStatuses map[string]InPlaceUpdateContainerStatus `json:"lastContainerStatuses"`
}

// InPlaceUpdateContainerStatus records the statuses of the container that are mainly used
// to determine whether the InPlaceUpdate is completed.
type InPlaceUpdateContainerStatus struct {
	ImageID string `json:"imageID,omitempty"`
}

//to find these container which will change in update and add they status before update
func getInPlaceUpdateStateJson(newPod, oldPod *corev1.Pod) error {
	inPlaceUpdateState := InPlaceUpdateState{
		LastContainerStatuses: make(map[string]InPlaceUpdateContainerStatus),
	}

	oldContainerImage := make(map[string]string)
	for _, container := range oldPod.Spec.Containers {
		oldContainerImage[container.Name] = container.Image
	}
	newContainerImage := make(map[string]string)
	for _, container := range newPod.Spec.Containers {
		newContainerImage[container.Name] = container.Image
	}

	for _, c := range oldPod.Status.ContainerStatuses {
		if oldContainerImage[c.Name] != newContainerImage[c.Name] {
			inPlaceUpdateState.LastContainerStatuses[c.Name] = InPlaceUpdateContainerStatus{
				ImageID: c.ImageID,
			}
		}
	}
	inPlaceUpdateStateJSON, err := json.Marshal(inPlaceUpdateState)
	if err != nil {
		return err
	}
	if newPod.Annotations == nil {
		newPod.Annotations = map[string]string{}
	}
	newPod.Annotations[InPlaceUpdateStateKey] = string(inPlaceUpdateStateJSON)
	return nil
}
