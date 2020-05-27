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
	"sort"
	"strconv"
	"time"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	clientset "tkestack.io/tapp/pkg/client/clientset/versioned"
	tappscheme "tkestack.io/tapp/pkg/client/clientset/versioned/scheme"
	informers "tkestack.io/tapp/pkg/client/informers/externalversions"
	listers "tkestack.io/tapp/pkg/client/listers/tappcontroller/v1"
	"tkestack.io/tapp/pkg/hash"
	"tkestack.io/tapp/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const controllerName = "tapp-controller"

const (
	// Time to sleep before polling to see if the pod cache has synced.
	PodStoreSyncedPollPeriod = 100 * time.Millisecond
	// number of retries for a status update.
	statusUpdateRetries = 2

	NodeUnreachablePodReason = "NodeLost"
)

var (
	deletePodAfterAppFinish = false
)

// Controller is the controller implementation for TApp resources
type Controller struct {
	// kubeclient is a standard kubernetes clientset
	kubeclient kubernetes.Interface
	// tappclient is a clientset for our own API group
	tappclient clientset.Interface

	tappLister  listers.TAppLister
	tappsSynced cache.InformerSynced

	tappHash hash.TappHashInterface

	// workqueue is a rate limited work workqueue. This is used to workqueue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	// newSyncer returns an interface capable of syncing a single instance.
	// Abstracted out for testing.
	syncer InstanceSyncer

	// podStore is a cache of watched pods.
	podStore corelisters.PodLister

	// podStoreSynced returns true if the pod store has synced at least once.
	podStoreSynced cache.InformerSynced

	// syncHandler handles sync events for tapps.
	// Abstracted as a func to allow injection for testing.
	syncHandler func(tappKey string) error
}

// NewController returns a new tapp controller
func NewController(
	kubeclientset kubernetes.Interface,
	tappclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	tappInformerFactory informers.SharedInformerFactory) *Controller {

	// obtain references to shared index informers for TApp and pod types.
	tappInformer := tappInformerFactory.Tappcontroller().V1().TApps()
	podInformer := kubeInformerFactory.Core().V1().Pods()

	// Create event broadcaster
	// Add tapp-controller types to the default Kubernetes Scheme so Events can be
	// logged for tapp-controller types.
	tappscheme.AddToScheme(scheme.Scheme)
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerName})

	controller := &Controller{
		kubeclient:  kubeclientset,
		tappclient:  tappclientset,
		tappLister:  tappInformer.Lister(),
		tappsSynced: tappInformer.Informer().HasSynced,
		tappHash:    hash.NewTappHash(),
		workqueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TApps"),
		recorder:    recorder,
		syncer: InstanceSyncer{
			InstanceClient: &ApiServerInstanceClient{
				KubeClient:            kubeclientset,
				Recorder:              recorder,
				pvcLister:             kubeInformerFactory.Core().V1().PersistentVolumeClaims().Lister(),
				InstanceHealthChecker: &defaultInstanceHealthChecker{},
			},
		},
	}

	klog.Info("Setting up event handlers")

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// lookup the tapp and enqueue
		AddFunc: controller.addPod,
		// lookup current and old tapp if labels changed
		UpdateFunc: controller.updatePod,
		// lookup tapp accounting for deletion tombstones
		DeleteFunc: controller.deletePod,
	})
	controller.podStore = podInformer.Lister()

	tappInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTApp,
		UpdateFunc: func(old, cur interface{}) {
			oldTApp := old.(*tappv1.TApp)
			curTApp := cur.(*tappv1.TApp)
			if oldTApp.Status.Replicas != curTApp.Status.Replicas {
				klog.V(4).Infof("Observed updated replica count for tapp %s: %d->%d",
					util.GetTAppFullName(curTApp), oldTApp.Status.Replicas, curTApp.Status.Replicas)
			}
			controller.enqueueTApp(cur)
		},
		DeleteFunc: controller.enqueueTApp,
	})

	controller.podStoreSynced = podInformer.Informer().HasSynced
	controller.syncHandler = controller.Sync

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting tapp controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.podStoreSynced, c.tappsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process TApp resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) GetEventRecorder() record.EventRecorder {
	return c.recorder
}

// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (c *Controller) runWorker() {
	for {
		func() {
			key, quit := c.workqueue.Get()
			if quit {
				return
			}
			defer c.workqueue.Done(key)
			if err := c.syncHandler(key.(string)); err != nil {
				klog.Errorf("Error syncing TApp %v, re-queuing: %v", key.(string), err)
				c.workqueue.AddRateLimited(key)
			} else {
				c.workqueue.Forget(key)
			}
		}()
	}
}

// addPod adds the tapp for the pod to the sync workqueue
func (c *Controller) addPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	klog.V(8).Infof("Pod %s created, labels: %+v", pod.Name, pod.Labels)
	if tapp, err := c.getTAppForPod(pod); err == nil {
		c.enqueueTApp(tapp)
	}
}

// updatePod adds the tapp for the current and old pods to the sync workqueue.
// If the labels of the pod didn't change, this method enqueues a single tapp.
func (c *Controller) updatePod(old, cur interface{}) {
	curPod := cur.(*corev1.Pod)
	oldPod := old.(*corev1.Pod)
	if curPod.ResourceVersion == oldPod.ResourceVersion {
		// Periodic resync will send update events for all known pods.
		// Two different versions of the same pod will always have different RVs.
		return
	}
	if tapp, err := c.getTAppForPod(curPod); err == nil {
		c.enqueueTApp(tapp)
	}
	if !reflect.DeepEqual(curPod.Labels, oldPod.Labels) {
		if oldTapp, err := c.getTAppForPod(oldPod); err == nil {
			c.enqueueTApp(oldTapp)
		}
	}
}

// deletePod enqueues the tapp for the pod accounting for deletion tombstones.
func (c *Controller) deletePod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)

	// When a delete is dropped, the relist will notice a pod in the store not
	// in the list, leading to the insertion of a tombstone object which contains
	// the deleted key/value. Note that this value might be stale. If the pod
	// changed labels the new TApp will not be woken up till the periodic resync.
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a pod %+v", obj)
			return
		}
	}
	klog.V(8).Infof("Pod %s/%s deleted.", pod.Namespace, pod.Name)
	if tapp, err := c.getTAppForPod(pod); err == nil {
		c.enqueueTApp(tapp)
	} else {
		klog.Errorf("Failed to get tapp for pod %s/%s", pod.Namespace, pod.Name)
	}
}

// getPodsForTApps returns the pods that match the selectors of the given tapp.
func (c *Controller) getPodsForTApp(tapp *tappv1.TApp) ([]*corev1.Pod, error) {
	sel, err := metav1.LabelSelectorAsSelector(tapp.Spec.Selector)
	if err != nil {
		return []*corev1.Pod{}, err
	}
	pods, err := c.podStore.Pods(tapp.Namespace).List(sel)
	if err != nil {
		return []*corev1.Pod{}, err
	}
	result := make([]*corev1.Pod, 0, len(pods))
	for i := range pods {
		result = append(result, &(*pods[i]))
	}
	return result, nil
}

// getTAppForPod returns the instance set managing the given pod.
func (c *Controller) getTAppForPod(pod *corev1.Pod) (*tappv1.TApp, error) {
	if len(pod.Labels) == 0 {
		return nil, fmt.Errorf("no TApps found for pod %v because it has no labels", pod.Name)
	}

	list, err := c.tappLister.TApps(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var tapps []*tappv1.TApp
	for _, l := range list {
		selector, err := metav1.LabelSelectorAsSelector(l.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("invalid selector: %v", err)
		}

		// If a tapp with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		tapps = append(tapps, l)
	}

	if len(tapps) == 0 {
		return nil, fmt.Errorf("could not find tapps for pod %s in namespace %s with labels: %v",
			pod.Name, pod.Namespace, pod.Labels)
	}

	// Resolve a overlapping tapp tie by creation timestamp.
	// Let's hope users don't create overlapping tapps.
	if len(tapps) > 1 {
		klog.Errorf("More than one TApp is selecting pods with labels: %+v", pod.Labels)
		sort.Sort(overlappingTApps(tapps))
	}
	return tapps[0], nil
}

// enqueueTApp enqueues the given tapp in the work workqueue.
func (c *Controller) enqueueTApp(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Could not get key for object %+v: %v", obj, err)
		return
	}
	c.workqueue.Add(key)
}

// Sync syncs the given tapp.
func (c *Controller) Sync(key string) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing tapp %s(%v)", key, time.Now().Sub(startTime))
	}()

	if !c.podStoreSynced() {
		klog.V(2).Infof("Pod store is not synced, skip syncing tapp %s", key)
		// Sleep to give the pod reflector goroutine a chance to run.
		time.Sleep(PodStoreSyncedPollPeriod)
		return fmt.Errorf("waiting for pods controller to sync")
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	tapp, err := c.tappLister.TApps(namespace).Get(name)
	if errors.IsNotFound(err) {
		klog.Infof("TApp has been deleted %v", key)
		return nil
	}
	if err != nil {
		klog.Errorf("Unable to retrieve tapp %s from store: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	err = c.preprocessTApp(tapp)
	if err != nil {
		klog.Errorf("Failed to preprocess tapp %s: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	pods, err := c.getPodsForTApp(tapp)
	if err != nil {
		klog.Errorf("Failed to get pods for tapp %s: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	if isTAppFinished(tapp) && tapp.Generation == tapp.Status.ObservedGeneration &&
		tapp.Spec.Replicas == tapp.Status.Replicas && len(pods) == 0 {
		klog.Errorf("Tapp %s has finished, replica: %d, status: %s", util.GetTAppFullName(tapp),
			tapp.Spec.Replicas, tapp.Status.AppStatus)
		return nil
	}

	c.syncTApp(tapp, pods)

	if err := c.updateTAppStatus(tapp, pods); err != nil {
		klog.Errorf("Failed to update tapp %s's status: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	return nil
}

// Mainly check whether we need create/add/delete templates, and do it if needed.
func (c *Controller) preprocessTApp(tapp *tappv1.TApp) error {
	var newTapp *tappv1.TApp
	var err error

	if tapp.Generation != tapp.Status.ObservedGeneration {
		klog.V(4).Infof("==> Pre-update tapp %+v", tapp)
		// TODO: it is a little tricky here, and it might brings unnecessary update.
		// It will take effect on calculating the tapp hash.
		// If container resource is 0.5 cpu, it will be updated to 500m after updating tapp because of serialization.
		newTapp, err = c.tappclient.TappcontrollerV1().TApps(tapp.Namespace).Update(tapp)
		if err != nil {
			return err
		}
	} else {
		newTapp = tapp.DeepCopy()
	}

	if err := c.updateTemplateHash(newTapp); err != nil {
		klog.Errorf("Failed to update template hash for tapp: %s", util.GetTAppFullName(tapp))
		return err
	}

	err = c.setLabelSelector(newTapp)
	if err != nil {
		klog.Errorf("Failed to setLabelSelector for tapp %s: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	// Set scale label selector
	err = c.setScaleLabelSelector(newTapp)
	if err != nil {
		klog.Errorf("Failed to setScaleLabelSelector for tapp %s: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	// Update tapp if needed
	if !reflect.DeepEqual(newTapp.Spec, tapp.Spec) {
		klog.V(4).Infof("Update tapp %s's spec: %+v => %+v", util.GetTAppFullName(tapp), tapp.Spec, newTapp.Spec)
		if newTapp, err = c.tappclient.TappcontrollerV1().TApps(newTapp.Namespace).Update(newTapp); err != nil {
			return err
		}
	}
	if !reflect.DeepEqual(newTapp.Status, tapp.Status) {
		klog.V(4).Infof("Update tapp %s's status: %+v => %+v", util.GetTAppFullName(tapp), tapp.Spec, newTapp.Spec)
		if newTapp, err = c.tappclient.TappcontrollerV1().TApps(newTapp.Namespace).UpdateStatus(newTapp); err != nil {
			return err
		}
	}
	newTapp.DeepCopyInto(tapp)

	return nil
}

// updateTemplateHash will generate and update templates hash if needed.
func (c *Controller) updateTemplateHash(tapp *tappv1.TApp) error {
	updateHash := func(template *corev1.PodTemplateSpec) {
		if c.tappHash.SetTemplateHash(template) {
			c.tappHash.SetUniqHash(template)
		}
	}

	updateHash(&tapp.Spec.Template)

	for _, template := range tapp.Spec.TemplatePool {
		updateHash(&template)
	}

	return nil
}

func (c *Controller) setLabelSelector(tapp *tappv1.TApp) error {
	if tapp.Spec.Selector != nil {
		return nil
	}

	if len(tapp.Spec.Template.Labels) == 0 {
		klog.Errorf("Found a tapp %s with no labels: %+v", tapp.Name, tapp.Spec)
		return fmt.Errorf("no lables found in tapp: %s", util.GetTAppFullName(tapp))
	}

	labels := tapp.Spec.Template.Labels

	// copy labels to tapp labels, tappHashKey is not supposed to be selector
	tappLabels := make(map[string]string, len(labels))
	for k, v := range labels {
		tappLabels[k] = v
	}
	for _, label := range c.tappHash.HashLabels() {
		delete(tappLabels, label)
	}

	tapp.Spec.Selector = util.GenerateTAppSelector(labels)

	if tapp.Labels == nil {
		tapp.Labels = tappLabels
	}

	return nil
}

func (c *Controller) setScaleLabelSelector(tapp *tappv1.TApp) error {
	selector, err := metav1.LabelSelectorAsSelector(tapp.Spec.Selector)
	if err != nil {
		klog.Errorf("Failed to get label selector for TApp %s: %v", util.GetTAppFullName(tapp), err)
		return err
	}

	str := selector.String()
	if tapp.Status.ScaleLabelSelector == str {
		return nil
	}

	tapp.Status.ScaleLabelSelector = str

	return nil
}

func isInstanceFinished(status tappv1.InstanceStatus) bool {
	return status == tappv1.InstanceKilled || status == tappv1.InstanceFailed ||
		status == tappv1.InstanceSucc
}

func getDesiredInstance(tapp *tappv1.TApp) (running, completed sets.String) {
	completed = sets.NewString()
	for id, status := range tapp.Spec.Statuses {
		// Instance is killed
		if status == tappv1.InstanceKilled {
			completed.Insert(id)
		}
	}

	// If `deletePodAfterAppFinish` is not enabled, pod will be deleted once instance finishes.
	if !getDeletePodAfterAppFinish() || isTAppFinished(tapp) {
		for id, status := range tapp.Status.Statuses {
			// Instance finished
			if isInstanceFinished(status) {
				completed.Insert(id)
			}
		}
	}

	total := sets.NewString()
	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		total.Insert(strconv.Itoa(i))
	}

	running = total.Difference(completed)
	return
}

// pod is supposed to running, but completed actually, should we restart it or leave it be
func shouldPodMigrate(tapp *tappv1.TApp, pod *corev1.Pod, id string) (bool, error) {
	migrate := true
	podTemplate, err := getPodTemplate(&tapp.Spec, id)
	if err != nil {
		return true, fmt.Errorf("failed to get pod template from tapp %s with id %s: %v",
			util.GetTAppFullName(tapp), id, err)
	}

	if tapp.Spec.NeverMigrate {
		return false, nil
	}

	// Containers in pod have not run yet, e.g. kubelet reject the pod.
	// TODO: It is just a workaround to add `len(pod.Spec.NodeName) != 0` to make old test cases pass.
	if len(pod.Spec.NodeName) != 0 && len(pod.Status.InitContainerStatuses) == 0 &&
		len(pod.Status.ContainerStatuses) == 0 {
		return true, nil
	}

	restartPolicy := podTemplate.Spec.RestartPolicy
	switch restartPolicy {
	case corev1.RestartPolicyAlways:
		migrate = true
	case corev1.RestartPolicyOnFailure:
		if isPodFailed(pod) {
			migrate = true
		} else {
			migrate = false
		}
	case corev1.RestartPolicyNever:
		migrate = false
	}
	return migrate, nil
}

func makePodMap(pods []*corev1.Pod) map[string]*corev1.Pod {
	podMap := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		index, err := getPodIndex(pod)
		if err != nil {
			klog.Errorf("Failed to get pod index from pod %s: %v", getPodFullName(pod), err)
			continue
		}
		podMap[index] = pod
	}
	return podMap
}

func (c *Controller) instanceToSync(tapp *tappv1.TApp, pods []*corev1.Pod) (add, del, forceDel, update []*Instance) {
	podMap := makePodMap(pods)
	running, completed := getDesiredInstance(tapp)

	add, del, forceDel, update = c.syncRunningInstances(tapp, running, podMap)
	a, d, f, u := c.syncCompletedInstances(tapp, completed, podMap)
	add = append(add, a...)
	del = append(del, d...)
	forceDel = append(forceDel, f...)
	update = append(update, u...)

	// Delete pods that exceed replica
	for id, pod := range podMap {
		if idInt, err := strconv.Atoi(id); err == nil {
			if idInt >= int(tapp.Spec.Replicas) {
				if isPodDying(pod) {
					continue
				}
				if instance, err := newInstanceWithPod(tapp, pod); err == nil {
					del = append(del, instance)
				}
			}
		}
	}
	return
}

func (c *Controller) syncRunningInstances(tapp *tappv1.TApp, running sets.String,
	podMap map[string]*corev1.Pod) (add, del, forceDel, update []*Instance) {
	for _, id := range running.List() {
		if pod, ok := podMap[id]; !ok {
			// pod does not exist, should create it
			if instance, err := newInstance(tapp, id); err == nil {
				add = append(add, instance)
			} else {
				klog.Errorf("Failed to newInstance %s-%s: %v", util.GetTAppFullName(tapp), id, err)
			}
		} else {
			if c.needForceDelete(tapp, pod) {
				// 0, pod is Deleting, we need force delete it for some cases
				klog.V(4).Infof("Force delete pod %s", getPodFullName(pod))
				if instance, err := newInstanceWithPod(tapp, pod); err == nil {
					forceDel = append(forceDel, instance)
				} else {
					klog.Errorf("Failed to newInstance %s: %+v", getPodFullName(pod), err)
				}
			} else if isPodDying(pod) {
				// 1, pod is Deleting, wait kubelet/nodeController to completely delete pod
			} else if isPodCompleted(pod) {
				// 2, pod is completed, migrate pod or just leave it be
				if migrate, err := shouldPodMigrate(tapp, pod, id); err != nil {
					klog.Errorf("Failed to determine whether needs migrate pod %s: %v", getPodFullName(pod), err)
				} else if migrate {
					klog.V(4).Infof("Migrating pod %s", getPodFullName(pod))
					if instance, err := newInstanceWithPod(tapp, pod); err == nil {
						del = append(del, instance)
					} else {
						klog.Errorf("Failed to newInstance %s: %v", getPodFullName(pod), err)
					}
				} else {
					klog.V(6).Infof("Skip migrating pod %s, status:%s", getPodFullName(pod), pod.Status.Phase)
				}
			} else if c.isTemplateHashChanged(tapp, id, pod) {
				// 3, pod is pending/running/unknown, update pod if template hash changed.
				// Pod is pending/running/unkonw, check whether template hash changes.
				// If template hash changes, it means some thing in pod spec got updated.
				// If image is updated, we only need restart corresponding container, otherwise
				// we need recreate the pod because k8s does not support restarting it.
				if !c.isUniqHashChanged(tapp, id, pod) {
					// Update pod
					if instance, err := newInstance(tapp, id); err == nil {
						update = append(update, instance)
					} else {
						klog.Errorf("Failed to newInstance %s-%s: %v", util.GetTAppFullName(tapp), id, err)
					}
				} else {
					// Recreate pod
					klog.V(4).Infof("Recreating instance %s", getPodFullName(pod))
					if instance, err := newInstanceWithPod(tapp, pod); err == nil {
						del = append(del, instance)
					} else {
						klog.Errorf("Failed to newInstance %s: %+v", getPodFullName(pod), err)
					}
				}
			}
		}
	}
	return
}

func (c *Controller) syncCompletedInstances(tapp *tappv1.TApp, completed sets.String,
	podMap map[string]*corev1.Pod) (add, del, forceDel, update []*Instance) {
	for _, id := range completed.List() {
		if pod, ok := podMap[id]; ok {
			if c.needForceDelete(tapp, pod) {
				klog.V(4).Infof("Failed to force delete pod %s", getPodFullName(pod))
				if instance, err := newInstanceWithPod(tapp, pod); err == nil {
					forceDel = append(forceDel, instance)
				} else {
					klog.Errorf("Failed to newInstance %s: %+v", getPodFullName(pod), err)
				}
			} else if isPodDying(pod) {
				// pod is deleting, wait for kubelet to completely delete
			} else if instance, err := newInstanceWithPod(tapp, pod); err == nil {
				// delete running or completed pod
				del = append(del, instance)
			}
		}
	}
	return
}

func (c *Controller) needForceDelete(tapp *tappv1.TApp, pod *corev1.Pod) bool {
	return isPodDying(pod) && pod.Status.Reason == NodeUnreachablePodReason && tapp.Spec.ForceDeletePod
}

func (c *Controller) isTemplateHashChanged(tapp *tappv1.TApp, podId string, pod *corev1.Pod) bool {
	hash := c.tappHash.GetTemplateHash(pod.Labels)

	template, err := getPodTemplate(&tapp.Spec, podId)
	if err != nil {
		klog.Errorf("Failed to get pod template for %s from tapp %s", getPodFullName(pod),
			util.GetTAppFullName(tapp))
		return true
	}
	expected := c.tappHash.GetTemplateHash(template.Labels)
	return hash != expected
}

func (c *Controller) isUniqHashChanged(tapp *tappv1.TApp, podId string, pod *corev1.Pod) bool {
	hash := c.tappHash.GetUniqHash(pod.Labels)
	template, err := getPodTemplate(&tapp.Spec, podId)
	if err != nil {
		klog.Errorf("Failed to get pod template for %s from tapp %s", getPodFullName(pod),
			util.GetTAppFullName(tapp))
		return true
	}
	expected := c.tappHash.GetUniqHash(template.Labels)
	return hash != expected
}

func getInstanceStatus(tapp *tappv1.TApp, pods []*corev1.Pod) map[string]tappv1.InstanceStatus {
	statuses := map[string]tappv1.InstanceStatus{}
	for _, pod := range pods {
		id, err := getPodIndex(pod)
		if err != nil {
			klog.Errorf("Failed to get pod %s's index: %v", getPodFullName(pod), err)
			continue
		}

		// For the case: if instance finished, tapp controller will try to delete it,
		// then `isPodDying(pod)` in the following code will be true, and it will be
		// set to InstanceKilling. At the next sync, pod is deleted, and status is
		// not finished, tapp controller will create the pod. :(
		if isPodDying(pod) && !isInstanceFinished(tapp.Status.Statuses[id]) {
			statuses[id] = tappv1.InstanceKilling
			continue
		}

		switch pod.Status.Phase {
		case corev1.PodPending:
			statuses[id] = tappv1.InstancePending
		case corev1.PodRunning:
			if isPodReady(pod) {
				statuses[id] = tappv1.InstanceRunning
			} else {
				statuses[id] = tappv1.InstancePending
			}
		case corev1.PodFailed:
			fallthrough
		case corev1.PodSucceeded:
			status := tappv1.InstanceSucc
			if pod.Status.Phase == corev1.PodFailed {
				status = tappv1.InstanceFailed
			}
			migrate, err := shouldPodMigrate(tapp, pod, id)
			if err != nil {
				klog.Errorf("Failed to determine whether need migrate pod %s: %v", getPodFullName(pod), err)
				statuses[id] = status
			}
			if migrate {
				if status == tappv1.InstanceSucc {
					statuses[id] = tappv1.InstancePodSucc
				} else {
					statuses[id] = tappv1.InstancePodFailed
				}
			} else {
				statuses[id] = status
			}
		case corev1.PodUnknown:
			statuses[id] = tappv1.InstanceUnknown
		}
	}

	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		id := strconv.Itoa(i)
		if _, ok := statuses[id]; !ok {
			if status, ok := tapp.Spec.Statuses[id]; ok {
				statuses[id] = status
			} else if isInstanceFinished(tapp.Status.Statuses[id]) {
				statuses[id] = tapp.Status.Statuses[id]
			} else {
				statuses[id] = tappv1.InstanceNotCreated
			}
		}
	}
	return statuses
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return true
}

func genAppStatus(tapp *tappv1.TApp) tappv1.AppStatus {
	if len(tapp.Status.Statuses) == 0 {
		return tappv1.AppPending
	}

	for _, status := range tapp.Status.Statuses {
		if isInstanceAlive(status) {
			return tappv1.AppRunning
		}
	}

	for _, status := range tapp.Status.Statuses {
		if status == tappv1.InstanceFailed {
			return tappv1.AppFailed
		}
	}

	for _, status := range tapp.Status.Statuses {
		if status == tappv1.InstanceSucc {
			return tappv1.AppSucc
		}
	}

	// now all instance are killed
	return tappv1.AppKilled
}

func shouldUpdateTAppStatus(tapp *tappv1.TApp, pods []*corev1.Pod) bool {
	if tapp.Generation != tapp.Status.ObservedGeneration {
		return true
	}
	statuses := getInstanceStatus(tapp, pods)
	if !reflect.DeepEqual(tapp.Status.Statuses, statuses) {
		return true
	}

	appStatus := genAppStatus(tapp)
	if appStatus != tapp.Status.AppStatus {
		return true
	}
	return false
}

func getTotalReplicas(statuses map[string]tappv1.InstanceStatus) (replica int32) {
	replica = 0
	for _, status := range statuses {
		if status != tappv1.InstanceNotCreated {
			replica++
		}
	}
	return
}

func getReadyReplicas(statuses map[string]tappv1.InstanceStatus) (replica int32) {
	replica = 0
	for _, status := range statuses {
		if status == tappv1.InstanceRunning {
			replica++
		}
	}
	return
}

func (c *Controller) updateTAppStatus(tapp *tappv1.TApp, pods []*corev1.Pod) error {
	if !shouldUpdateTAppStatus(tapp, pods) {
		klog.V(4).Infof("No need to update tapp %s's status", util.GetTAppFullName(tapp))
		return nil
	}

	client := c.tappclient.TappcontrollerV1().TApps(tapp.Namespace)

	var getErr error
	var updateErr error
	for i, app := 0, tapp; ; i++ {
		statuses := getInstanceStatus(app, pods)
		app.Status.Statuses = statuses
		app.Status.Replicas = getTotalReplicas(app.Status.Statuses)
		app.Status.ReadyReplicas = getReadyReplicas(app.Status.Statuses)
		app.Status.ObservedGeneration = app.Generation
		app.Status.AppStatus = genAppStatus(app)
		klog.V(3).Infof("Updating tapp %s's status: replicas:%+v, status:%+v, pods:%+v", util.GetTAppFullName(app),
			app.Status.Replicas, app.Status.AppStatus, app.Status.Statuses)
		_, updateErr = client.UpdateStatus(app)
		if updateErr != nil {
			klog.Errorf("Failed to update status for %s: %+v", app.Name, updateErr)
		}
		if updateErr == nil || i >= statusUpdateRetries {
			return updateErr
		}
		if app, getErr = client.Get(app.Name, metav1.GetOptions{}); getErr != nil {
			return getErr
		}
	}
}

func rollUpdateFilter(tapp *tappv1.TApp, pods []*corev1.Pod, updates, dels []*Instance,
	updating map[string]bool) []*Instance {
	if tapp.Spec.UpdateStrategy.MaxUnavailable == nil {
		return updates
	}

	podMap := makePodMap(pods)
	delMap := map[string]bool{}
	for _, instance := range dels {
		delMap[instance.id] = true
	}

	updateMap := map[string]string{}
	rollingTemplateName := getRollingTemplateKey(tapp)
	rollingUpdates := InstanceSortWithId{}
	forceUpdates := []*Instance{}
	for _, instance := range updates {
		templateName := getPodTemplateName(tapp.Spec.Templates, instance.id)
		updateMap[instance.id] = templateName
		if templateName == rollingTemplateName {
			rollingUpdates = append(rollingUpdates, instance)
		} else {
			forceUpdates = append(forceUpdates, instance)
		}
	}

	realRunning := 0
	killNum := 0
	// get the pod is running and will be not delete/update in next op
	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		id := strconv.Itoa(i)
		if delMap[id] || tapp.Spec.Statuses[id] == tappv1.InstanceKilled {
			killNum++
			continue
		}

		if templateId, ok := updateMap[id]; ok {
			if templateId != rollingTemplateName {
				continue
			}
		}

		pod, ok := podMap[id]
		if !ok {
			continue
		}
		if isPodInActive(pod) || updating[id] {
			continue
		}

		if _, condition := GetPodCondition(&pod.Status, corev1.PodReady); condition != nil {
			if condition.Status == corev1.ConditionTrue {
				realRunning++
			}
		}
	}

	minRunning := int(tapp.Spec.Replicas) - killNum - int(*tapp.Spec.UpdateStrategy.MaxUnavailable)
	if realRunning <= minRunning {
		klog.V(3).Infof("Stop rolling update tapp %s, realRunning:%d <= minRunning:%d, expectUpdates:%v, realUpdates:%v",
			util.GetTAppFullName(tapp), realRunning, minRunning, extractInstanceId(updates), extractInstanceId(forceUpdates))
		return forceUpdates
	}

	sort.Sort(rollingUpdates)
	for i := 0; i < realRunning-minRunning && i < len(rollingUpdates); i++ {
		forceUpdates = append(forceUpdates, rollingUpdates[i])
	}

	if len(forceUpdates) != len(updates) {
		klog.V(2).Infof("Rolling update tapp %s, realRunning:%d, minRunning:%d, expectUpdates:%v, realUpdates:%v",
			util.GetTAppFullName(tapp), realRunning, minRunning, extractInstanceId(updates), extractInstanceId(forceUpdates))
	}
	return forceUpdates
}

func (c *Controller) syncTApp(tapp *tappv1.TApp, pods []*corev1.Pod) {
	klog.V(4).Infof("Syncing tapp %s with %d pods", util.GetTAppFullName(tapp), len(pods))
	add, del, forceDel, update := c.instanceToSync(tapp, pods)

	if shouldRollUpdate(tapp, update) {
		updating := getUpdatingPods(pods)
		update = rollUpdateFilter(tapp, pods, update, del, updating)
	}
	c.syncer.SyncInstances(add, del, forceDel, update)
}

// getUpdatingPods returns a map whose key is pod id(e.g. "0", "1"), the map records updating pods
func getUpdatingPods(pods []*corev1.Pod) map[string]bool {
	updating := make(map[string]bool)
	for _, pod := range pods {
		if isUpdating(pod) {
			if id, err := getPodIndex(pod); err != nil {
				klog.Errorf("Failed to get pod %s/%s index: %v", pod.Namespace, pod.Name, err)
			} else {
				updating[id] = true
			}
		}
	}
	return updating
}

// isUpdating returns true if kubelet is updating image for pod, otherwise returns false
func isUpdating(pod *corev1.Pod) bool {
	isSameImage := func(expected, real string) bool {
		return expected == real || "docker.io/"+expected == real ||
			"docker.io/"+expected+":latest" == real || expected+":latest" == real
	}
	expectedContainerImage := make(map[string]string)
	for _, container := range pod.Spec.Containers {
		expectedContainerImage[container.Name] = container.Image
	}
	for _, status := range pod.Status.ContainerStatuses {
		if expectedImage, found := expectedContainerImage[status.Name]; !found {
			klog.Warningf("Failed to find expected image for container %s in pod %s/%s",
				status.Name, pod.Namespace, pod.Name)
		} else if !isSameImage(expectedImage, status.Image) || len(status.ImageID) == 0 {
			// status.ImageID == "" means pulling image now
			klog.V(5).Infof("Pod %s/%s is updating", pod.Namespace, pod.Name)
			return true
		}
	}

	return false
}

func SetDeletePodAfterAppFinish(value bool) {
	deletePodAfterAppFinish = value
}

func getDeletePodAfterAppFinish() bool {
	return deletePodAfterAppFinish
}
