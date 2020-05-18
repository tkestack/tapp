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
	"strconv"
	"testing"
	"time"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	"tkestack.io/tapp/pkg/hash"
	"tkestack.io/tapp/pkg/testutil"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newFakeTAppController() (*Controller, *fakeInstanceClient) {
	fakeClient := newFakeInstanceClient()
	return &Controller{
		kubeclient:     nil,
		podStoreSynced: func() bool { return true },
		tappHash:       hash.NewTappHash(),
		syncer:         InstanceSyncer{InstanceClient: fakeClient},
	}, fakeClient
}

func checkInstances(name string, tapp *tappv1.TApp, creates, deletes, forceDeletes, updates int, fc *fakeInstanceClient, t *testing.T) {
	if fc.InstancesCreated != creates || fc.InstancesDeleted != deletes || fc.InstanceForceDeleted != forceDeletes || fc.InstancesUpdated != updates {
		t.Errorf("Test %s found (creates: %d, deletes: %d, forceDeletes: %d, updates: %d), expected (creates: %d, deletes: %d, forceDeletes: %d, updates: %d)",
			name, fc.InstancesCreated, fc.InstancesDeleted, fc.InstanceForceDeleted, fc.InstancesUpdated, creates, deletes, forceDeletes, updates)
	}

	for id := range fc.Instances {
		expectedInstance, _ := newInstance(tapp, id)
		if identityHash(tapp, fc.Instances[id].pod) != identityHash(tapp, expectedInstance.pod) {
			t.Errorf("Test %s unexpected instance :%s", name, id)
		}
	}
}

func syncTApp(t *testing.T, tapp *tappv1.TApp, controller *Controller, fc *fakeInstanceClient) {
	pl := fc.getPodList()
	controller.syncTApp(tapp, pl)
}

func deployTApp(t *testing.T, replica int) *tappv1.TApp {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(replica)
	syncTApp(t, tapp, controller, client)
	checkInstances("deployTApp", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)
	return tapp
}

func TestDeployTApp(t *testing.T) {
	deployTApp(t, 2)
}

func TestKillInstance(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestKillInstance1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	testutil.KillInstance(tapp, "0")
	oldCreate := client.InstancesCreated
	oldDelete := client.InstancesDeleted
	syncTApp(t, tapp, controller, client)
	checkInstances("TestKillInstance2", tapp, oldCreate, oldDelete+1, 0, 0, client, t)
}

func TestRestartInstance(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestRestartInstance1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	testutil.KillInstance(tapp, "0")
	syncTApp(t, tapp, controller, client)

	testutil.RestartInstance(tapp, "0")
	oldCreate := client.InstancesCreated
	oldDelete := client.InstancesDeleted
	syncTApp(t, tapp, controller, client)
	checkInstances("TestRestartInstance2", tapp, oldCreate+1, oldDelete, 0, 0, client, t)

	// Set instance to succ
	tapp.Status.Statuses["1"] = tappv1.InstanceFailed
	syncTApp(t, tapp, controller, client)

	testutil.RestartInstance(tapp, "1")
	oldCreate = client.InstancesCreated
	oldDelete = client.InstancesDeleted
	syncTApp(t, tapp, controller, client)
	checkInstances("TestRestartInstance3", tapp, oldCreate+1, oldDelete, 0, 0, client, t)
}

func TestUpdateInstance(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestUpdateInstance1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	// Update image, we need update pod.
	template := testutil.CreateValidPodTemplate()
	image := template.Spec.Containers[0].Image
	template.Spec.Containers[0].Image = image + "update"
	template0 := "template0"
	if err := testutil.AddPodTemplate(tapp, template0, template); err != nil {
		t.Errorf("add pod template failed,%v", err)
	}

	testutil.UpdateInstanceTemplate(tapp, "0", template0)
	oldCreate := client.InstancesCreated
	oldDelete := client.InstancesDeleted
	oldUpdate := client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestUpdateInstance2", tapp, oldCreate, oldDelete, 0, oldUpdate+1, client, t)

	// Update restart policy, we need recreate pod.
	template = testutil.CreateValidPodTemplate()
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	template1 := "template1"
	if err := testutil.AddPodTemplate(tapp, template1, template); err != nil {
		t.Errorf("add pod template failed,%v", err)
	}

	testutil.UpdateInstanceTemplate(tapp, "1", template1)
	oldCreate = client.InstancesCreated
	oldDelete = client.InstancesDeleted
	oldUpdate = client.InstancesUpdated
	// First delete the instance
	syncTApp(t, tapp, controller, client)
	checkInstances("TestUpdateInstance3_1", tapp, oldCreate, oldDelete+1, 0, oldUpdate, client, t)
	// Then create the instance
	syncTApp(t, tapp, controller, client)
	checkInstances("TestUpdateInstance3_2", tapp, oldCreate+1, oldDelete+1, 0, oldUpdate, client, t)
}

func TestInstanceFailed(t *testing.T) {
	onFail := func() *corev1.PodTemplateSpec {
		template := testutil.CreateValidPodTemplate()
		template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
		return template
	}
	always := func() *corev1.PodTemplateSpec {
		template := testutil.CreateValidPodTemplate()
		template.Spec.RestartPolicy = corev1.RestartPolicyAlways
		return template
	}
	never := func() *corev1.PodTemplateSpec {
		template := testutil.CreateValidPodTemplate()
		template.Spec.RestartPolicy = corev1.RestartPolicyNever
		return template
	}

	controller, client := newFakeTAppController()
	tapp := testutil.CreateTAppWithTemplateCreater(2, always)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	client.setPodStatus("0", corev1.PodFailed)
	oldCreate, oldDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed2", tapp, oldCreate+1, oldDelete+1, 0, oldUpdate, client, t)

	controller, client = newFakeTAppController()
	tapp = testutil.CreateTAppWithTemplateCreater(2, onFail)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed3", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	client.setPodStatus("0", corev1.PodFailed)
	oldCreate, oldDelete, oldUpdate = client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed4", tapp, oldCreate+1, oldDelete+1, 0, oldUpdate, client, t)

	controller, client = newFakeTAppController()
	tapp = testutil.CreateTAppWithTemplateCreater(2, never)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed5", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	client.setPodStatus("0", corev1.PodFailed)
	oldCreate, oldDelete, oldUpdate = client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceFailed6", tapp, oldCreate, oldDelete, 0, oldUpdate, client, t)
}

func TestInstanceSucc(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceSucc1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	client.setPodStatus("0", corev1.PodSucceeded)
	oldCreate, oldDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceSucc2", tapp, oldCreate, oldDelete, 0, oldUpdate, client, t)
}

func TestInstanceDeleted(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceDeleted1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	client.DeleteInstance("0")
	oldCreate, oldDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceDeleted2", tapp, oldCreate+1, oldDelete, 0, oldUpdate, client, t)
}

func TestInstanceForceDeleted(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestTestInstanceForceDeleted1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	timeout := 5 * time.Minute
	client.setDeletionTimestamp("0", time.Now().Add(-timeout-1))
	oldCreate, oldDelete, oldForceDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstanceForceDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceDeleted2", tapp, oldCreate, oldDelete, oldForceDelete, oldUpdate, client, t)

	bak := tapp.Spec.ForceDeletePod
	tapp.Spec.ForceDeletePod = true
	client.setPodReason("0", NodeUnreachablePodReason)
	oldForceDelete = client.InstanceForceDeleted
	syncTApp(t, tapp, controller, client)
	checkInstances("TestInstanceDeleted3", tapp, oldCreate, oldDelete, oldForceDelete+1, oldUpdate, client, t)
	if client.InstanceForceDeleted != oldForceDelete+1 {
		t.Errorf("TestInstanceDeleted3: need force delete a pod")
	}
	tapp.Spec.ForceDeletePod = bak
}

func TestRampUp(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestRampUp1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	template := testutil.CreateValidPodTemplate()
	image := template.Spec.Containers[0].Image
	template.Spec.Containers[0].Image = image + "update"
	ramupTemplate := "ramup"
	if err := testutil.AddPodTemplate(tapp, ramupTemplate, template); err != nil {
		t.Errorf("add pod template failed,%v", err)
	}
	testutil.RampUp(tapp, uint(int(tapp.Spec.Replicas)+2), ramupTemplate)
	oldCreate := client.InstancesCreated
	oldDelete := client.InstancesDeleted
	oldUpdate := client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestRampUp2", tapp, oldCreate+2, oldDelete, 0, oldUpdate, client, t)
}

func TestShrinkDown(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(4)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestShrinkDown1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	testutil.ShrinkDown(tapp, uint(int(tapp.Spec.Replicas)-3))
	oldCreate, oldDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestShrinkDown2", tapp, oldCreate, oldDelete+3, 0, oldUpdate, client, t)
}

func TestKillApp(t *testing.T) {
	controller, client := newFakeTAppController()
	tapp := testutil.CreateValidTApp(2)
	syncTApp(t, tapp, controller, client)
	checkInstances("TestKillApp1", tapp, int(tapp.Spec.Replicas), 0, 0, 0, client, t)

	testutil.KillAllInstance(tapp)
	oldCreate, oldDelete, oldUpdate := client.InstancesCreated, client.InstancesDeleted, client.InstancesUpdated
	syncTApp(t, tapp, controller, client)
	checkInstances("TestKillApp2", tapp, oldCreate, oldDelete+2, 0, oldUpdate, client, t)
}

type AppStatusTest struct {
	instanceStatuses []tappv1.InstanceStatus
	appStatus        tappv1.AppStatus
	// if set to true, add any instanceStatus to instanceStatues, appStatus should not changed
	withAny bool
}

func testAppStatus(t *testing.T, status map[string]tappv1.InstanceStatus, appStatus tappv1.AppStatus) {
	tapp := testutil.CreateValidTApp(1)
	tapp.Status.Statuses = status
	realAppStatus := genAppStatus(tapp)
	if appStatus != realAppStatus {
		t.Errorf("instanceStatus:%v, expected app status:%s, real app status:%s", status, appStatus, realAppStatus)
	}
}

func TestAppStatus(t *testing.T) {
	tests := []AppStatusTest{
		{
			instanceStatuses: []tappv1.InstanceStatus{},
			appStatus:        tappv1.AppPending,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceNotCreated},
			appStatus:        tappv1.AppRunning,
			withAny:          true,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstancePending},
			appStatus:        tappv1.AppRunning,
			withAny:          true,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceRunning},
			appStatus:        tappv1.AppRunning,
			withAny:          true,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceKilling},
			appStatus:        tappv1.AppRunning,
			withAny:          true,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceKilled},
			appStatus:        tappv1.AppKilled,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceSucc},
			appStatus:        tappv1.AppSucc,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceFailed},
			appStatus:        tappv1.AppFailed,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceUnknown},
			appStatus:        tappv1.AppRunning,
			withAny:          true,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceKilled, tappv1.InstanceFailed},
			appStatus:        tappv1.AppFailed,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceSucc, tappv1.InstanceFailed},
			appStatus:        tappv1.AppFailed,
		},
		{
			instanceStatuses: []tappv1.InstanceStatus{tappv1.InstanceKilled, tappv1.InstanceSucc},
			appStatus:        tappv1.AppSucc,
		},
	}

	for _, test := range tests {
		statuses := map[string]tappv1.InstanceStatus{}
		for id, status := range test.instanceStatuses {
			statuses[strconv.Itoa(id)] = status
		}
		testAppStatus(t, statuses, test.appStatus)

		if test.withAny {
			for _, status := range tappv1.InstanceStatusAll {
				newStatuses := map[string]tappv1.InstanceStatus{}
				for k, v := range statuses {
					newStatuses[k] = v
				}
				newStatuses[strconv.Itoa(len(statuses))] = status
				testAppStatus(t, newStatuses, test.appStatus)
			}
		}
	}
}

func testInstanceStatus(t *testing.T, id string, expected, real tappv1.InstanceStatus) {
	if expected != real {
		t.Errorf("instance id :%s, expected status:%s, real status:%s", id, expected, real)
	}
}

func TestInstanceStatus(t *testing.T) {
	replica := 16
	tapp := testutil.CreateValidTApp(replica)

	pods := buildPods(tapp)
	newPods := []*corev1.Pod{}
	for _, pod := range pods {
		index, _ := getPodIndex(pod)
		switch index {
		//NOT CREATED
		case "0":
			continue
		case "1":
			pod.Status.Phase = corev1.PodPending
		case "2":
			pod.Status.Phase = corev1.PodRunning
		case "3":
			pod.Status.Phase = corev1.PodSucceeded
		case "4":
			pod.Status.Phase = corev1.PodFailed
		case "5":
			pod.Status.Phase = corev1.PodUnknown
		// killing
		case "6":
			pod.Status.Phase = corev1.PodRunning
			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		// running
		case "7":
			pod.Status.Phase = corev1.PodRunning
			tapp.Spec.Statuses[index] = tappv1.InstanceKilled
		case "8":
			tapp.Spec.Statuses[index] = tappv1.InstanceKilled
			continue
		case "9":
			tapp.Status.Statuses[index] = tappv1.InstanceFailed
			continue
		case "10":
			tapp.Status.Statuses[index] = tappv1.InstanceSucc
			continue
		// not created
		case "11":
			tapp.Status.Statuses[index] = tappv1.InstanceRunning
			continue
		// InstanceSucc, but pod is dying because we'll delete pod after it finishes.
		case "12":
			pod.Status.Phase = corev1.PodSucceeded
			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			tapp.Status.Statuses[index] = tappv1.InstanceSucc
		// running, and pod is Ready
		case "13":
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = append(pod.Status.Conditions,
				corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionTrue})
		// running, and pod is not Ready
		case "14":
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = append(pod.Status.Conditions,
				corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse})
		// Restart a killed pod
		case "15":
			pod.Status.Phase = corev1.PodRunning
			tapp.Status.Statuses[index] = tappv1.InstanceKilled
		}
		newPods = append(newPods, pod)
	}

	statuses := getInstanceStatus(tapp, newPods)
	tapp.Status.Statuses = statuses

	expectedStatuses := map[string]tappv1.InstanceStatus{"0": tappv1.InstanceNotCreated, "1": tappv1.InstancePending,
		"2": tappv1.InstanceRunning, "3": tappv1.InstanceSucc, "4": tappv1.InstancePodFailed, "5": tappv1.InstanceUnknown,
		"6": tappv1.InstanceKilling, "7": tappv1.InstanceRunning, "8": tappv1.InstanceKilled, "9": tappv1.InstanceFailed,
		"10": tappv1.InstanceSucc, "11": tappv1.InstanceNotCreated, "12": tappv1.InstanceSucc, "13": tappv1.InstanceRunning,
		"14": tappv1.InstancePending, "15": tappv1.InstanceRunning,
	}
	for id, status := range tapp.Status.Statuses {
		testInstanceStatus(t, id, expectedStatuses[id], status)
	}
}

func buildPods(tapp *tappv1.TApp) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, tapp.Spec.Replicas)
	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		if instance, err := newInstance(tapp, strconv.Itoa(i)); err == nil {
			pods = append(pods, instance.pod)
		}
	}
	return pods
}

type RollUpdateTestUnit struct {
	states  []InstanceTestState
	updates []int
}

func (u *RollUpdateTestUnit) test(t *testing.T) {
	tapp, pods, updates, dels, err := createRollUpdateTestValues(u.states)
	if err != nil {
		t.Errorf("%+v", err)
	}

	updating := map[string]bool{}

	newUpdates := rollUpdateFilter(tapp, pods, updates, dels, updating)

	if len(newUpdates) != len(u.updates) {
		t.Fatalf("testUnit:%+v, pods:%v, newUpdates:%v, updates:%v, expected:%v",
			u, extractPodStatus(pods), extractInstanceId(newUpdates), extractInstanceId(updates), u.updates)
	}

	for i, instance := range newUpdates {
		if instance.id != strconv.Itoa(u.updates[i]) {
			t.Fatalf("testUnit:%+v, newUpdates:%v, expected:%v", u, extractInstanceId(newUpdates), u.updates)
		}
	}
}

type InstanceTestState string

const (
	// RUNNING
	READY    InstanceTestState = "Ready"
	NOTREADY InstanceTestState = "NotReady"

	UPDATE    InstanceTestState = "Update"
	ROLLUPATE InstanceTestState = "Rollupdate"

	DEADING InstanceTestState = "Deading"

	KILLED   InstanceTestState = "Killed"
	COMPLETE InstanceTestState = "Complete"

	NULL InstanceTestState = "nil"
)

func createTAppWithRollUpdate(replica int) (*tappv1.TApp, string, string, error) {
	tapp := testutil.CreateValidTApp(replica)
	rollUpdateTemplate := "rollupdate"
	forceUpdateTemplate := "forceupdate"

	template := testutil.CreateValidPodTemplate()
	image := template.Spec.Containers[0].Image
	template.Spec.Containers[0].Image = image + "roll_update"
	if err := testutil.AddPodTemplate(tapp, rollUpdateTemplate, template); err != nil {
		return nil, "", "", fmt.Errorf("add pod template failed,%v", err)
	}

	if tapp.Annotations == nil {
		tapp.Annotations = make(map[string]string)
	}
	tapp.Spec.UpdateStrategy.Template = rollUpdateTemplate
	tapp.Spec.UpdateStrategy.MaxUnavailable = new(int32)
	*tapp.Spec.UpdateStrategy.MaxUnavailable = 1

	template = testutil.CreateValidPodTemplate()
	image = template.Spec.Containers[0].Image
	template.Spec.Containers[0].Image = image + "force_update"
	if err := testutil.AddPodTemplate(tapp, forceUpdateTemplate, template); err != nil {
		return nil, "", "", fmt.Errorf("add pod template failed,%v", err)
	}

	return tapp, rollUpdateTemplate, forceUpdateTemplate, nil
}

func createTAppPod(tapp *tappv1.TApp, id string, phase corev1.PodPhase, readyStatus corev1.ConditionStatus) *Instance {
	instance, _ := newInstance(tapp, id)
	pod := instance.pod
	pod.Status.Phase = phase
	pod.Status.Conditions = make([]corev1.PodCondition, 1)
	pod.Status.Conditions[0] = corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: readyStatus,
	}
	return instance
}

func extractPodStatus(pods []*corev1.Pod) []InstanceTestState {
	states := []InstanceTestState{}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			states = append(states, DEADING)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			_, condition := GetPodCondition(&pod.Status, corev1.PodReady)
			if condition == nil {
				states = append(states, NULL)
			} else if condition.Status == corev1.ConditionTrue {
				states = append(states, READY)
			} else {
				states = append(states, NOTREADY)
			}
		case corev1.PodFailed:
			fallthrough
		case corev1.PodSucceeded:
			states = append(states, COMPLETE)
		}
	}

	return states
}

func createRollUpdateTestValues(instances []InstanceTestState) (*tappv1.TApp, []*corev1.Pod, []*Instance, []*Instance, error) {
	replica := len(instances)
	tapp, rollUpdateId, forceUpdateId, err := createTAppWithRollUpdate(replica)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	pods := []*corev1.Pod{}
	updates := []*Instance{}
	dels := []*Instance{}
	for i, state := range instances {
		id := strconv.Itoa(i)
		switch state {
		case KILLED:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionTrue)
			pods = append(pods, instance.pod)
			dels = append(dels, instance)
			testutil.KillInstance(tapp, id)
		case UPDATE:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionTrue)
			pods = append(pods, instance.pod)
			updates = append(updates, instance)
			testutil.UpdateInstanceTemplate(tapp, id, forceUpdateId)
		case ROLLUPATE:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionTrue)
			pods = append(pods, instance.pod)
			updates = append(updates, instance)
			testutil.UpdateInstanceTemplate(tapp, id, rollUpdateId)
		case READY:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionTrue)
			pods = append(pods, instance.pod)
		case NOTREADY:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionFalse)
			pods = append(pods, instance.pod)
		case DEADING:
			instance := createTAppPod(tapp, id, corev1.PodRunning, corev1.ConditionTrue)
			instance.pod.DeletionTimestamp = &metav1.Time{time.Now()}
			pods = append(pods, instance.pod)
		case COMPLETE:
			instance := createTAppPod(tapp, id, corev1.PodFailed, corev1.ConditionFalse)
			pods = append(pods, instance.pod)
		default:
		}
	}

	return tapp, pods, updates, dels, nil
}

func TestRollingUpdate(t *testing.T) {
	tests := []RollUpdateTestUnit{
		// no effect to normal
		{
			[]InstanceTestState{READY, READY, READY},
			[]int{},
		},
		{
			[]InstanceTestState{READY, READY, NOTREADY},
			[]int{},
		},
		{
			[]InstanceTestState{READY, READY, UPDATE},
			[]int{2},
		},
		{
			[]InstanceTestState{READY, READY, DEADING},
			[]int{},
		},
		{
			[]InstanceTestState{READY, READY, KILLED},
			[]int{},
		},
		{
			[]InstanceTestState{READY, READY, COMPLETE},
			[]int{},
		},
		// add a rollupdate
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, READY},
			[]int{0},
		},
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, NOTREADY},
			[]int{},
		},
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, UPDATE},
			[]int{3},
		},
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, DEADING},
			[]int{},
		},
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, KILLED},
			[]int{0},
		},
		{
			[]InstanceTestState{ROLLUPATE, READY, READY, COMPLETE},
			[]int{},
		},
		// add 2 rollupdate
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, READY},
			[]int{0},
		},
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, NOTREADY},
			[]int{},
		},
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, UPDATE},
			[]int{4},
		},
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, DEADING},
			[]int{},
		},
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, KILLED},
			[]int{0},
		},
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, COMPLETE},
			[]int{},
		},
		// more UPDATE
		{
			[]InstanceTestState{ROLLUPATE, ROLLUPATE, READY, READY, UPDATE, UPDATE},
			[]int{4, 5},
		},
	}

	for _, unit := range tests {
		unit.test(t)
	}
}
