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
	"crypto/md5"
	"fmt"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	corev1 "k8s.io/api/core/v1"
)

// identityMapper is an interface for assigning identities to a instance.
// All existing identity mappers just append "-(index)" to the tapp name to
// generate a unique identity.
// There's a more elegant way to achieve this mapping, but we're
// taking the simplest route till we have data on whether users will need
// more customization.
// Note that running a single identity mapper is not guaranteed to give
// your instance a unique identity. You must run them all. Order doesn't matter.
type identityMapper interface {
	// SetIdentity takes an id and assigns the given instance an identity based
	// on the tapp spec. The is must be unique amongst members of the
	// tapp.
	SetIdentity(id string, pod *corev1.Pod)

	// Identity returns the identity of the instance.
	Identity(pod *corev1.Pod) string
}

func newIdentityMappers(tapp *tappv1.TApp) []identityMapper {
	return []identityMapper{
		&NameIdentityMapper{tapp},
	}
}

// NameIdentityMapper assigns names to instances.
// It also puts the instance in the same namespace as the parent.
type NameIdentityMapper struct {
	tapp *tappv1.TApp
}

// SetIdentity sets the instance namespace, name, hostname and subdomain.
func (n *NameIdentityMapper) SetIdentity(id string, pod *corev1.Pod) {
	pod.Name = fmt.Sprintf("%v-%v", n.tapp.Name, id)
	pod.Namespace = n.tapp.Namespace
	pod.Labels[tappv1.TAppInstanceKey] = id

	if n.tapp.Spec.ServiceName != "" {
		pod.Spec.Hostname = pod.Name
		pod.Spec.Subdomain = n.tapp.Spec.ServiceName
	}

	return
}

// Identity returns the name identity of the instance.
func (n *NameIdentityMapper) Identity(instance *corev1.Pod) string {
	return n.String(instance)
}

// String is a string function for the name identity of the instance.
func (n *NameIdentityMapper) String(instance *corev1.Pod) string {
	return fmt.Sprintf("%v/%v", instance.Namespace, instance.Name)
}

// identityHash computes a hash of the instance by running all the above identity
// mappers.
func identityHash(tapp *tappv1.TApp, pod *corev1.Pod) string {
	id := ""
	for _, idMapper := range newIdentityMappers(tapp) {
		id += idMapper.Identity(pod)
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(id)))
}
