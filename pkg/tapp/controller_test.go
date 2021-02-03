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
	"reflect"
	"testing"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	"tkestack.io/tapp/pkg/client/clientset/versioned/fake"
	"tkestack.io/tapp/pkg/testutil"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	core "k8s.io/client-go/testing"
)

const (
	RUNNING    = "Running"
	WAITING    = "Waiting"
	TERMINATED = "Terminated"
)

func newContainerStatus(name, state string, exitcode, restartCount int) corev1.ContainerStatus {
	status := corev1.ContainerStatus{Name: name, RestartCount: int32(restartCount)}
	var containerState corev1.ContainerState
	switch state {
	case RUNNING:
		containerState.Running = &corev1.ContainerStateRunning{}
	case TERMINATED:
		containerState.Terminated = &corev1.ContainerStateTerminated{
			ExitCode: int32(exitcode),
		}
	case WAITING:
		containerState.Waiting = &corev1.ContainerStateWaiting{}
		status.LastTerminationState = corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				ExitCode: int32(exitcode),
			},
		}
	}
	status.State = containerState
	return status
}

func TestGetDesiredInstance(t *testing.T) {
	replica := 12
	tapp := testutil.CreateValidTApp(replica)

	statuses := tapp.Status.Statuses

	tapp.Spec.Statuses["0"] = tappv1.InstanceKilled
	statuses["1"] = tappv1.InstanceNotCreated
	statuses["2"] = tappv1.InstancePending
	statuses["3"] = tappv1.InstanceRunning
	statuses["4"] = tappv1.InstanceUpdating
	statuses["5"] = tappv1.InstancePodFailed
	statuses["6"] = tappv1.InstancePodSucc
	statuses["7"] = tappv1.InstanceKilling
	statuses["8"] = tappv1.InstanceKilled
	statuses["9"] = tappv1.InstanceFailed
	statuses["10"] = tappv1.InstanceSucc
	statuses["11"] = tappv1.InstanceUnknown

	cases := []struct {
		Name                          string
		AppStatus                     tappv1.AppStatus
		EnableDeletePodAfterAppFinish bool
		ExpectedRunning               []string
		ExpectedCompleted             []string
	}{
		{
			Name:                          "TestGetDesiredInstance1",
			AppStatus:                     tappv1.AppRunning,
			EnableDeletePodAfterAppFinish: false,
			ExpectedRunning:               []string{"1", "2", "3", "4", "5", "6", "7", "8", "11"},
			ExpectedCompleted:             []string{"0", "9", "10"},
		},
		{
			Name:                          "TestGetDesiredInstance2",
			AppStatus:                     tappv1.AppRunning,
			EnableDeletePodAfterAppFinish: true,
			ExpectedRunning:               []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
			ExpectedCompleted:             []string{"0"},
		},
		{
			Name:                          "TestGetDesiredInstance2",
			AppStatus:                     tappv1.AppSucc,
			EnableDeletePodAfterAppFinish: true,
			ExpectedRunning:               []string{"1", "2", "3", "4", "5", "6", "7", "8", "11"},
			ExpectedCompleted:             []string{"0", "9", "10"},
		},
	}

	deletePodAfterAppFinishBak := getDeletePodAfterAppFinish()

	for _, test := range cases {
		tapp.Status.AppStatus = test.AppStatus
		SetDeletePodAfterAppFinish(test.EnableDeletePodAfterAppFinish)
		running, completed := getDesiredInstance(tapp)
		for _, id := range test.ExpectedRunning {
			if !running.Has(id) {
				t.Errorf("Pod %s expected status: running, got: completed", id)
			}
		}
		for _, id := range test.ExpectedCompleted {
			if !completed.Has(id) {
				t.Errorf("Pod %s expected status: running, got: completed", id)
			}
		}
	}
	SetDeletePodAfterAppFinish(deletePodAfterAppFinishBak)
}

func TestShouldPodMigrate(t *testing.T) {
	tapp := tappv1.TApp{
		Spec: tappv1.TAppSpec{
			Templates: map[string]string{"0": "8888"},
			TemplatePool: map[string]corev1.PodTemplateSpec{
				"8888": {Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever}},
			},
		},
	}
	pod := corev1.Pod{
		Spec:   corev1.PodSpec{NodeName: "test-node"},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	v, err := shouldPodMigrate(&tapp, &pod, "0")
	if err != nil || v != true {
		t.Errorf("TestShouldPodMigrate failed for pod that no containers have run")
	}

	// Test case for 'NeverMigrate'
	tapp.Spec.NeverMigrate = true
	v, err = shouldPodMigrate(&tapp, &pod, "0")
	if err != nil || v != false {
		t.Errorf("TestShouldPodMigrate failed for pod that no containers have run")
	}
}

func TestSetScaleLabelSelector(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("update", "*", func(action core.Action) (bool, runtime.Object, error) {
		if updateAction, ok := action.(core.UpdateAction); ok {
			return true, updateAction.GetObject(), nil
		} else {
			return false, nil, fmt.Errorf("not an update action")
		}
	})
	c := Controller{tappclient: client}

	cases := []struct {
		Name             string
		Tapp             *tappv1.TApp
		ExpectedSelector string
	}{
		{
			Name: "TestSetScaleLabelSelector1",
			Tapp: &tappv1.TApp{
				Spec: tappv1.TAppSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "nginx"},
					},
				},
			},
			ExpectedSelector: "app=nginx",
		},
		{
			Name: "TestSetScaleLabelSelector2",
			Tapp: &tappv1.TApp{
				Spec: tappv1.TAppSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "nginx", "type": "tapp", "version": "v1"},
					},
				},
				Status: tappv1.TAppStatus{
					ScaleLabelSelector: "app=nginx",
				},
			},
			ExpectedSelector: "app=nginx,type=tapp,version=v1",
		},
	}

	for _, test := range cases {
		err := c.setScaleLabelSelector(test.Tapp)
		if err != nil || test.Tapp.Status.ScaleLabelSelector != test.ExpectedSelector {
			t.Errorf("%s failed, expected %s, got %s", test.Name, test.ExpectedSelector, test.Tapp.Status.ScaleLabelSelector)
		}
	}
}

/* TODO: enable it
func TestDoKubectlApply(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("update", "*", func(action core.Action) (bool, runtime.Object, error) {
		if updateAction, ok := action.(core.UpdateAction); ok {
			return true, updateAction.GetObject(), nil
		} else {
			return false, nil, fmt.Errorf("not an update action")
		}
	})
	c := Controller{tappclient: client}

	tapp := &tappv1.TApp{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nginx",
		},
		Spec: tappv1.TAppSpec{
			Replicas: 2,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":           "nginx",
						"tapp_name_key": "nginx",
						TAppHashKey:     "17685766981161034909",
						TAppUniqHashKey: "3226707109688322824",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
						},
					},
				},
			},
			TemplatePool: map[string]corev1.PodTemplateSpec{
				"17685766981161034909": {
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":           "nginx",
							"tapp_name_key": "nginx",
							TAppHashKey:     "11674099963091183259",
							TAppUniqHashKey: "3226707109688322824",
						}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx",
							},
						},
					},
				},
			},
			Templates: map[string]string{
				"0": "17685766981161034909",
				"1": "17685766981161034909",
			},
		},
	}

	// case 0: image changed
	tapp2 := tapp.DeepCopy()
	tapp2.Spec.Template.Spec.Containers[0].Image = "nginx:1.7.9"
	expectedTapp2 := tapp2.DeepCopy()
	tappHash := tappUtil.GeneratePodTemplateSpecHash(expectedTapp2.Spec.Template)
	tappUniqHash := tappUtil.GenerateUniqPodTemplateSpecHash(expectedTapp2.Spec.Template)
	expectedTapp2.Spec.Template.Labels[TAppHashKey] = tappHash
	expectedTapp2.Spec.Template.Labels[TAppUniqHashKey] = tappUniqHash
	expectedTapp2.Spec.TemplatePool[tappv1.DefaultTemplateName] = expectedTapp2.Spec.Template
	expectedTapp2.Spec.Templates["0"] = tappv1.DefaultTemplateName
	expectedTapp2.Spec.Templates["1"] = tappv1.DefaultTemplateName

	// case 1: add a filed
	tapp3 := tapp.DeepCopy()
	tapp3.Spec.Template.Spec.Containers[0].ImagePullPolicy = "Never"
	expectedTapp3 := tapp3.DeepCopy()
	tappHash = tappUtil.GeneratePodTemplateSpecHash(expectedTapp3.Spec.Template)
	tappUniqHash = tappUtil.GenerateUniqPodTemplateSpecHash(expectedTapp3.Spec.Template)
	expectedTapp3.Spec.Template.Labels[TAppHashKey] = tappHash
	expectedTapp3.Spec.Template.Labels[TAppUniqHashKey] = tappUniqHash
	expectedTapp3.Spec.TemplatePool[tappv1.DefaultTemplateName] = expectedTapp3.Spec.Template
	expectedTapp3.Spec.Templates["0"] = tappv1.DefaultTemplateName
	expectedTapp3.Spec.Templates["1"] = tappv1.DefaultTemplateName

	cases := []struct {
		Name         string
		Tapp         *tappv1.TApp
		ExpectedTapp *tappv1.TApp
	}{
		{
			Name:         "TestDoKubectlApply2",
			Tapp:         tapp2,
			ExpectedTapp: expectedTapp2,
		},
		{
			Name:         "TestDoKubectlApply3",
			Tapp:         tapp3,
			ExpectedTapp: expectedTapp3,
		},
	}
	for _, test := range cases {
		c.doKubectlApply(test.Tapp)
		//t.Errorf("%+v", test.Tapp.Spec)
		if !reflect.DeepEqual(test.ExpectedTapp.Spec, test.Tapp.Spec) {
			t.Errorf("%s failed, expected %+v, got %+v", test.Name, test.ExpectedTapp, test.Tapp)
		}
	}

}
*/

func TestIsUpdatingPods(t *testing.T) {
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
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{tappv1.TAppInstanceKey: "1"},
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
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:    "containerA",
					Image:   "nginx",
					ImageID: "123456",
				},
				{
					Name:  "containerB",
					Image: "2048", // No image ID
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
					Image: "2048",
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:    "containerA",
					Image:   "nginx", // Image has not been updated
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
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{tappv1.TAppInstanceKey: "3"},
			Annotations: map[string]string{InPlaceUpdateStateKey: InPlaceUpdateStateValue},
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
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:    "containerA",
					Image:   "docker.io/nginx:1.7.9",
					ImageID: "1234567",
				},
				{
					Name:    "containerB",
					Image:   "2048:latest",
					ImageID: "123456",
				},
			},
		},
	}
	pod4 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{tappv1.TAppInstanceKey: "4"},
			Annotations: map[string]string{InPlaceUpdateStateKey: InPlaceUpdateStateValue},
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
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:    "containerA",
					Image:   "nginx",
					ImageID: "1234567",
				},
				{
					Name:    "containerB",
					Image:   "2048:latest",
					ImageID: "123456",
				},
			},
		},
	}
	pods := []*corev1.Pod{pod0, pod1, pod2, pod3, pod4}
	expectedUpdating := map[string]bool{"4": true}
	updating := getUpdatingPods(pods)
	if !reflect.DeepEqual(expectedUpdating, updating) {
		t.Errorf("Failed to getUpdatingPods, expected: %+v, got: %+v", expectedUpdating, updating)
	}
}

// getUpdatingPods returns a map whose key is pod id(e.g. "0", "1"), the map records updating pods
func getUpdatingPods(pods []*corev1.Pod) map[string]bool {
	updating := make(map[string]bool)
	for _, pod := range pods {
		if isUpdating(pod) {
			if id, err := getPodIndex(pod); err == nil {
				updating[id] = true
			}
		}
	}
	return updating
}
