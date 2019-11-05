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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
)

type FakeKubeClient struct {
	kubeclient *kubernetes.Interface
}

func (client *FakeKubeClient) Apps() {

}

func newFakeInstanceClient() *fakeInstanceClient {
	return &fakeInstanceClient{
		Instances:             map[string]*Instance{},
		recorder:              &record.FakeRecorder{},
		InstanceHealthChecker: &defaultInstanceHealthChecker{},
	}
}

type fakeInstanceClient struct {
	Instances                                                                  map[string]*Instance
	InstancesCreated, InstancesDeleted, InstanceForceDeleted, InstancesUpdated int
	recorder                                                                   record.EventRecorder
	InstanceHealthChecker
}

// Delete fakes Instance client deletion.
func (f *fakeInstanceClient) Delete(p *Instance, options *metav1.DeleteOptions) error {
	if _, ok := f.Instances[p.id]; ok {
		delete(f.Instances, p.id)
		f.recorder.Eventf(p.parent, corev1.EventTypeNormal, "SuccessfulDelete", "Instance: %v", p.pod.Name)
	} else {
		return fmt.Errorf("failed to delete: instance %v doesn't exist", p.id)
	}
	if options == nil || (options.GracePeriodSeconds != nil && *options.GracePeriodSeconds != 0) {
		f.InstancesDeleted++
	} else {
		f.InstanceForceDeleted++
	}
	return nil
}

// Get fakes getting Instances.
func (f *fakeInstanceClient) Get(p *Instance) (*Instance, bool, error) {
	if instance, ok := f.Instances[p.id]; ok {
		return instance, true, nil
	}
	return nil, false, nil
}

// Create fakes Instance creation.
func (f *fakeInstanceClient) Create(p *Instance) error {
	if _, ok := f.Instances[p.id]; ok {
		return fmt.Errorf("failedl to create: instance %v already exists", p.id)
	}
	f.recorder.Eventf(p.parent, corev1.EventTypeNormal, "SuccessfulCreate", "Instance: %v", p.pod.Name)
	f.Instances[p.id] = p
	f.InstancesCreated++
	return nil
}

// Update fakes Instance updates.
func (f *fakeInstanceClient) Update(expected, wanted *Instance) error {
	if _, ok := f.Instances[wanted.id]; !ok {
		return fmt.Errorf("failed to update: Instance %v not found", wanted.id)
	}
	f.Instances[wanted.id] = wanted
	f.InstancesUpdated++
	return nil
}

func (f *fakeInstanceClient) getPodList() []*corev1.Pod {
	p := []*corev1.Pod{}
	for i, Instance := range f.Instances {
		if Instance.pod == nil {
			continue
		}
		p = append(p, f.Instances[i].pod)
	}
	return p
}

// Delete fakes Instance client deletion.
func (f *fakeInstanceClient) DeleteInstance(id string) error {
	if _, ok := f.Instances[id]; ok {
		delete(f.Instances, id)
	} else {
		return fmt.Errorf("delete failed: instance %v doesn't exist", id)
	}
	return nil
}

func (f *fakeInstanceClient) setDeletionTimestamp(id string, t time.Time) error {
	if _, ok := f.Instances[id]; !ok {
		return fmt.Errorf("instance %v not found", id)
	}
	f.Instances[id].pod.DeletionTimestamp = &metav1.Time{Time: t}
	return nil
}

func (f *fakeInstanceClient) setPodStatus(id string, status corev1.PodPhase) error {
	if _, ok := f.Instances[id]; !ok {
		return fmt.Errorf("instance %v not found", id)
	}
	f.Instances[id].pod.Status.Phase = status
	return nil
}

func (f *fakeInstanceClient) setPodReason(id string, reason string) error {
	if _, ok := f.Instances[id]; !ok {
		return fmt.Errorf("instance %v not found", id)
	}
	f.Instances[id].pod.Status.Reason = reason
	return nil
}
