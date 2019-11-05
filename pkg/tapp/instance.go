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
	"strings"
	"sync"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	"tkestack.io/tapp/pkg/util"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	errorutils "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
)

const (
	// updateRetries is the number of Get/Update cycles we perform when an
	// update fails.
	updateRetries = 3
)

// instance is the control block used to transmit all updates about a single instance.
// It serves as the manifest for a single instance. Users must populate the pod
// and parent fields to pass it around safely.
type Instance struct {
	// pod is the desired pod.
	pod *corev1.Pod
	// id is the identity index of this instance.
	id string
	// parent is a pointer to the parent tapp.
	parent *tappv1.TApp
}

func (instance *Instance) getName() string {
	if instance.parent != nil {
		return util.GetTAppFullName(instance.parent) + "-" + instance.id
	} else {
		return "unknown" + "-" + instance.id
	}
}

type InstanceSyncer struct {
	InstanceClient
}

func newInstanceWithPod(tapp *tappv1.TApp, pod *corev1.Pod) (*Instance, error) {
	if id, err := getPodIndex(pod); err == nil {
		return &Instance{pod, id, tapp}, nil
	} else {
		return nil, err
	}
}

func getTAppKind() schema.GroupVersionKind {
	return tappv1.SchemeGroupVersion.WithKind("TApp")
}

func newInstance(tapp *tappv1.TApp, id string) (*Instance, error) {
	template, err := getPodTemplate(&tapp.Spec, id)
	if err != nil {
		return nil, err
	}

	pod, err := util.GetPodFromTemplate(template, tapp, getControllerRef(tapp))
	if err != nil {
		return nil, err
	}
	for _, im := range newIdentityMappers(tapp) {
		im.SetIdentity(id, pod)
	}

	ins := &Instance{pod, id, tapp}
	updateStorage(ins)

	return ins, nil
}

func getControllerRef(tapp *tappv1.TApp) *metav1.OwnerReference {
	trueVar := true
	return &metav1.OwnerReference{
		APIVersion: getTAppKind().GroupVersion().String(),
		Kind:       getTAppKind().Kind,
		Name:       tapp.Name,
		UID:        tapp.UID,
		Controller: &trueVar,
	}
}

// updateStorage updates pod's Volumes to conform with the PersistentVolumeClaim of tapp's templates.
// If pod has conflicting local Volumes these are replaced with Volumes that conform to the tapp's templates.
func updateStorage(ins *Instance) {
	currentVolumes := ins.pod.Spec.Volumes
	claims := getPersistentVolumeClaims(ins)
	newVolumes := make([]corev1.Volume, 0, len(claims))
	for name, claim := range claims {
		newVolumes = append(newVolumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: claim.Name,
					// TODO: Use source definition to set this value when we have one.
					ReadOnly: false,
				},
			},
		})
	}
	for i := range currentVolumes {
		if _, ok := claims[currentVolumes[i].Name]; !ok {
			newVolumes = append(newVolumes, currentVolumes[i])
		}
	}
	ins.pod.Spec.Volumes = newVolumes
}

func (p *InstanceSyncer) SyncInstances(add, del, forceDel, update []*Instance) {
	var wg sync.WaitGroup
	wg.Add(len(add) + len(del) + len(forceDel))
	for _, instance := range add {
		go func(instance *Instance) {
			defer wg.Done()
			if err := p.createInstance(instance); err != nil {
				glog.Errorf("Failed to createInstance %s: %+v", instance.getName(), err)
			} else {
				glog.V(2).Infof("Create instance %s successfully", instance.getName())
			}
		}(instance)
	}

	for _, instance := range del {
		go func(instance *Instance) {
			defer wg.Done()
			if err := p.deleteInstance(instance); err != nil {
				glog.Errorf("Failed to delInstance %s: %v", instance.getName(), err)
			} else {
				glog.V(2).Infof("Delete instance %s successfully", instance.getName())
			}
		}(instance)
	}

	for _, instance := range forceDel {
		go func(instance *Instance) {
			defer wg.Done()
			if err := p.forceDeleteInstance(instance); err != nil {
				glog.Errorf("Failed to forceDelInstance %s: %v", instance.getName(), err)
			} else {
				glog.V(2).Infof("Force delete instance %s successfully", instance.getName())
			}
		}(instance)
	}

	wg.Wait()

	for _, instance := range update {
		if err := p.updateInstance(instance); err != nil {
			glog.Errorf("Failed to updateInstance %s: %v", instance.getName(), err)
		} else {
			glog.V(2).Infof("Update instance %s successfully", instance.getName())
		}
	}
}

func (syncer *InstanceSyncer) createInstance(ins *Instance) error {
	if ins == nil {
		return nil
	}
	_, exists, err := syncer.Get(ins)
	if err != nil {
		return err
	}

	if exists {
		return fmt.Errorf("instance exits, should delete first")
	}
	if err := syncer.Create(ins); err != nil {
		return err
	}
	return nil
}

// Delete deletes the given instance
func (p *InstanceSyncer) deleteInstance(ins *Instance) error {
	if ins == nil {
		return nil
	}
	real, exists, err := p.Get(ins)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	// This is counted as a delete, even if it fails.
	if !p.isDying(real.pod) {
		return p.InstanceClient.Delete(real, nil)
	}
	glog.V(2).Infof("Waiting on instance %s to die in %v", ins.getName(), real.pod.DeletionTimestamp)
	return nil
}

// Force delete deletes the given instance
func (p *InstanceSyncer) forceDeleteInstance(ins *Instance) error {
	if ins == nil {
		return nil
	}
	real, exists, err := p.Get(ins)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return p.InstanceClient.Delete(real, metav1.NewDeleteOptions(0))
}

func (p *InstanceSyncer) updateInstance(ins *Instance) error {
	real, exists, err := p.Get(ins)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("instance:%s not exist", ins.getName())
	}
	return p.Update(real, ins)
}

// InstanceClient is a client for managing instances.
type InstanceClient interface {
	InstanceHealthChecker
	Delete(*Instance, *metav1.DeleteOptions) error
	Get(*Instance) (*Instance, bool, error)
	Create(*Instance) error
	Update(*Instance, *Instance) error
}

// ApiServerinstanceClient is a instance aware Kubernetes client.
type ApiServerInstanceClient struct {
	KubeClient kubernetes.Interface
	Recorder   record.EventRecorder
	pvcLister  corelisters.PersistentVolumeClaimLister
	InstanceHealthChecker
}

func (p *ApiServerInstanceClient) Get(ins *Instance) (*Instance, bool, error) {
	found := true
	ns := ins.parent.Namespace
	pod, err := podClient(p.KubeClient, ns).Get(ins.pod.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		found = false
		err = nil
	}
	if err != nil || !found {
		return nil, found, err
	}
	real := *ins
	real.pod = pod
	return &real, true, nil
}

func (p *ApiServerInstanceClient) Delete(ins *Instance, options *metav1.DeleteOptions) error {
	glog.V(2).Infof("Delete instance %s with option %+v", ins.getName(), options)
	err := podClient(p.KubeClient, ins.parent.Namespace).Delete(ins.pod.Name, options)
	if errors.IsNotFound(err) {
		err = nil
	}
	p.event(ins.parent, "Delete", fmt.Sprintf("instance: %v", ins.pod.Name), err)
	return err
}

func (p *ApiServerInstanceClient) Create(ins *Instance) error {
	glog.V(2).Infof("Creating instance %s", ins.getName())
	if err := p.createPersistentVolumeClaims(ins); err != nil {
		return err
	}
	_, err := podClient(p.KubeClient, ins.parent.Namespace).Create(ins.pod)
	p.event(ins.parent, "Create", fmt.Sprintf("Instance: %v", ins.pod.Name), err)
	return err
}

func (p *ApiServerInstanceClient) createPersistentVolumeClaims(ins *Instance) error {
	var errs []error
	for _, claim := range getPersistentVolumeClaims(ins) {
		_, err := p.pvcLister.PersistentVolumeClaims(claim.Namespace).Get(claim.Name)
		switch {
		case apierrors.IsNotFound(err):
			_, createErr := pvcClient(p.KubeClient, claim.Namespace).Create(&claim)
			if createErr != nil {
				errs = append(errs, fmt.Errorf("failed to create PVC %s: %s", claim.Name, createErr))
			}
			if createErr == nil || !apierrors.IsAlreadyExists(createErr) {
				p.recordClaimEvent("create", ins, &claim, createErr)
			}
		case err != nil:
			errs = append(errs, fmt.Errorf("failed to retrieve PVC %s: %s", claim.Name, err))
			p.recordClaimEvent("create", ins, &claim, err)
		}
		// TODO: Check resource requirements and accessmodes, update if necessary
	}
	return errorutils.NewAggregate(errs)
}

// getPersistentVolumeClaims gets a map of PersistentVolumeClaims to their template names, as defined in set. The
// returned PersistentVolumeClaims are each constructed with a the name specific to the Pod. This name is determined
// by getPersistentVolumeClaimName.
func getPersistentVolumeClaims(ins *Instance) map[string]corev1.PersistentVolumeClaim {
	templates := ins.parent.Spec.VolumeClaimTemplates
	claims := make(map[string]corev1.PersistentVolumeClaim, len(templates))
	for i := range templates {
		claim := templates[i]
		claim.Name = getPersistentVolumeClaimName(ins.parent, &claim, ins.id)
		claim.Namespace = ins.parent.Namespace
		if claim.Labels == nil {
			claim.Labels = make(map[string]string)
		}
		for labelKey, labelValue := range ins.parent.Spec.Selector.MatchLabels {
			claim.Labels[labelKey] = labelValue
		}
		claim.OwnerReferences = append(claim.OwnerReferences, *getControllerRef(ins.parent))
		claims[templates[i].Name] = claim
	}
	return claims
}

// getPersistentVolumeClaimName gets the name of PersistentVolumeClaim for a Pod with an index of id.
// claim must be a PersistentVolumeClaim from tapp's VolumeClaims template.
func getPersistentVolumeClaimName(tapp *tappv1.TApp, claim *corev1.PersistentVolumeClaim, id string) string {
	// NOTE: This name format is used by the heuristics for zone spreading in ChooseZoneForVolume
	return fmt.Sprintf("%s-%s-%s", claim.Name, tapp.Name, id)
}

// recordClaimEvent records an event for verb applied to the PersistentVolumeClaim of a Pod in a TApp. If err is
// nil the generated event will have a reason of v1.EventTypeNormal. If err is not nil the generated event will have a
// reason of v1.EventTypeWarning.
func (p *ApiServerInstanceClient) recordClaimEvent(verb string, ins *Instance, claim *corev1.PersistentVolumeClaim,
	err error) {
	if err == nil {
		reason := fmt.Sprintf("Successful%s", strings.Title(verb))
		message := fmt.Sprintf("%s Claim %s Pod %s in StatefulSet %s success",
			strings.ToLower(verb), claim.Name, ins.pod.Name, ins.parent.Name)
		p.Recorder.Event(ins.parent, corev1.EventTypeNormal, reason, message)
	} else {
		reason := fmt.Sprintf("Failed%s", strings.Title(verb))
		message := fmt.Sprintf("%s Claim %s for Pod %s in TApp %s failed error: %s",
			strings.ToLower(verb), claim.Name, ins.pod.Name, ins.parent.Name, err)
		p.Recorder.Event(ins.parent, corev1.EventTypeWarning, reason, message)
	}
}

// api#validate grants the diff between real and excepted podTemplate are container image
// note: some admin controll plugins may change pod spec, such as ServiceAccount plugin will add
// vollumeMount to pod.Spec.Containers, so we couldn't simple use real.Sepc.Containers = expected.Spec.Containers
func mergePod(real, excepted *corev1.Pod) {
	newContainers := make([]corev1.Container, len(real.Spec.Containers))
	for index, container := range real.Spec.Containers {
		if index < len(excepted.Spec.Containers) {
			e := excepted.Spec.Containers[index]
			container.Image = e.Image
		}
		newContainers[index] = container
	}
	real.Spec.Containers = newContainers
	for k, v := range excepted.Labels {
		real.Labels[k] = v
	}
	if real.Annotations == nil {
		real.Annotations = make(map[string]string)
	}
	for k, v := range excepted.Annotations {
		real.Annotations[k] = v
	}
}

// TODO: Allow updating for VolumeClaimTemplates?
func (p *ApiServerInstanceClient) Update(real *Instance, expected *Instance) error {
	pc := podClient(p.KubeClient, expected.parent.Namespace)

	var err error
	pod := real.pod
	for i, rp := 0, real.pod; i <= updateRetries; i++ {
		mergePod(rp, expected.pod)
		glog.V(2).Infof("Updating pod %s, pod meta:%+v, pod spec:%+v", getPodFullName(rp), rp.ObjectMeta, rp.Spec)
		_, err = pc.Update(rp)
		if err == nil {
			p.event(real.parent, "Update", fmt.Sprintf("Instance: %v", real.pod.Name), nil)
			return nil
		}
		glog.Errorf("Failed to update pod %s, will retry: %v", getPodFullName(rp), err)
		if rp, err = pc.Get(pod.Name, metav1.GetOptions{}); err != nil {
			break
		}
	}
	p.event(real.parent, "Update", fmt.Sprintf("Instance: %v", real.pod.Name), err)
	return err
}

// event formats an event for the given runtime object.
func (p *ApiServerInstanceClient) event(obj runtime.Object, reason, msg string, err error) {
	if err != nil {
		p.Recorder.Eventf(obj, corev1.EventTypeWarning, fmt.Sprintf("Failed%v", reason),
			fmt.Sprintf("%v, error: %v", msg, err))
	} else {
		p.Recorder.Eventf(obj, corev1.EventTypeNormal, fmt.Sprintf("Successful%v", reason), msg)
	}
}

// InstanceHealthChecker is an interface to check instance health. It makes a boolean
// decision based on the given pod.
type InstanceHealthChecker interface {
	isDying(*corev1.Pod) bool
}

// defaultInstanceHealthChecks does basic health checking.
// It doesn't update, probe or get the pod.
type defaultInstanceHealthChecker struct{}

func (d *defaultInstanceHealthChecker) isDying(pod *corev1.Pod) bool {
	return pod != nil && pod.DeletionTimestamp != nil
}

type InstanceSortWithId []*Instance

func (o InstanceSortWithId) Len() int      { return len(o) }
func (o InstanceSortWithId) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o InstanceSortWithId) Less(i, j int) bool {
	id1, _ := strconv.Atoi(o[i].id)
	id2, _ := strconv.Atoi(o[j].id)
	return id1 < id2
}
