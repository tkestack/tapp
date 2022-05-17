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

package testutil

import (
	"fmt"
	"strconv"

	v1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	hashv1 "tkestack.io/tapp/pkg/hash/v1"
	"tkestack.io/tapp/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FakeTAppName   = "fake-tapp-name"
	FakeLabelKey   = "fake_tapp_label_key"
	FakeLabelValue = "fake_tapp_label_value"
)

var tappHash = hashv1.NewTappHash()

func AddPodTemplate(tapp *v1.TApp, templateName string, template *corev1.PodTemplateSpec) error {
	template = template.DeepCopy()
	tappHash.SetTemplateHash(template)
	tappHash.SetUniqHash(template)

	if tapp.Spec.TemplatePool == nil {
		tapp.Spec.TemplatePool = make(map[string]corev1.PodTemplateSpec)
	}
	tapp.Spec.TemplatePool[templateName] = *template

	if tappHash.GetTemplateHash(tapp.Spec.Template.Labels) != tappHash.GetTemplateHash(template.Labels) {
		tapp.Spec.Template = *template
	}

	return nil
}

func RampUp(tapp *v1.TApp, replica uint, templateName string) error {
	if replica <= uint(tapp.Spec.Replicas) {
		return fmt.Errorf("replica:%d should great than tapp.Spec.Replicas:%d", replica, tapp.Spec.Replicas)
	}

	if tapp.Spec.Templates == nil {
		tapp.Spec.Templates = make(map[string]string)
	}

	for i := int(tapp.Spec.Replicas); i < int(replica); i++ {
		id := strconv.Itoa(i)
		tapp.Spec.Templates[id] = templateName
	}
	tapp.Spec.Replicas = int32(replica)
	return nil
}

func ShrinkDown(tapp *v1.TApp, replica uint) error {
	if replica >= uint(tapp.Spec.Replicas) {
		return fmt.Errorf("replica:%d should less than tapp.Spec.Replicas:%d", replica, tapp.Spec.Replicas)
	}

	tapp.Spec.Replicas = int32(replica)

	for i := int(replica); i < int(tapp.Spec.Replicas); i++ {
		id := strconv.Itoa(i)
		delete(tapp.Spec.Templates, id)
		if tapp.Spec.Statuses != nil {
			delete(tapp.Spec.Statuses, id)
		}
	}

	return nil
}

func UpdateInstanceTemplate(tapp *v1.TApp, instanceId string, templateId string) error {
	if tapp.Spec.Templates == nil {
		tapp.Spec.Templates = map[string]string{}
	}
	// other validate is done in corev1server v1#validate.go
	tapp.Spec.Templates[instanceId] = templateId
	return nil
}

func KillInstance(tapp *v1.TApp, instanceId string) {
	if tapp.Spec.Statuses == nil {
		tapp.Spec.Statuses = map[string]v1.InstanceStatus{}
	}
	tapp.Spec.Statuses[instanceId] = v1.InstanceKilled
}

func KillAllInstance(tapp *v1.TApp) {
	if tapp.Spec.Statuses == nil {
		tapp.Spec.Statuses = map[string]v1.InstanceStatus{}
	}
	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		id := strconv.Itoa(i)
		if _, ok := tapp.Spec.Statuses[id]; !ok {
			tapp.Spec.Statuses[id] = v1.InstanceKilled
		}
	}
}

// 1. delete corresponding pod from apiserver
// 2. call restartInstance
// return true if tapp is updated
func RestartInstance(tapp *v1.TApp, instanceId string) bool {
	if tapp.Spec.Statuses != nil {
		if status, ok := tapp.Spec.Statuses[instanceId]; ok {
			if status == v1.InstanceKilled {
				delete(tapp.Spec.Statuses, instanceId)
			}
		}
	}
	// Delete instance status
	delete(tapp.Status.Statuses, instanceId)

	return true
}

func CreateValidPodTemplate() *corev1.PodTemplateSpec {
	validLabels := map[string]string{FakeLabelKey: FakeLabelValue}
	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      validLabels,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyOnFailure,
			DNSPolicy:     corev1.DNSClusterFirst,
			Containers:    []corev1.Container{{Name: "abc", Image: "image", ImagePullPolicy: "IfNotPresent"}},
		},
	}
}

func CreateRawTApp(replica int) *v1.TApp {
	validTAppSpec := v1.TAppSpec{
		Replicas:     int32(replica),
		TemplatePool: map[string]corev1.PodTemplateSpec{},
		Statuses:     map[string]v1.InstanceStatus{},
		Templates:    map[string]string{},
	}

	return &v1.TApp{
		TypeMeta:   metav1.TypeMeta{Kind: "TApp", APIVersion: "apps.tkestack.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: FakeTAppName, Namespace: corev1.NamespaceDefault, ResourceVersion: "0"},
		Spec:       validTAppSpec,
		Status:     v1.TAppStatus{Statuses: map[string]v1.InstanceStatus{}},
	}
}

type PodTemplateCreater func() *corev1.PodTemplateSpec

func CreateTAppWithTemplateCreater(replica int, creater PodTemplateCreater) *v1.TApp {
	tapp := CreateRawTApp(replica)
	template := creater()
	testTemplate := "testTemplate"
	if err := AddPodTemplate(tapp, testTemplate, template); err != nil {
		panic(err)
	}

	tapp.Spec.Selector = util.GenerateTAppSelector(tapp.Spec.Template.Labels)

	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		id := strconv.Itoa(i)
		tapp.Spec.Templates[id] = testTemplate
	}
	return tapp
}

func CreateValidTApp(replica int) *v1.TApp {
	return CreateTAppWithTemplateCreater(replica, CreateValidPodTemplate)
}
