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
	"sync"
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

type PodAction string

const (
	createPod   PodAction = "CREATE"
	updatePod   PodAction = "UPDATE"
	recreatePod PodAction = "RECREATE"
	deletePod   PodAction = "DELETE"
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
		// TODO: does it impact performance?
		result = append(result, pods[i].DeepCopy())
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
		klog.V(4).Infof("Pre-update tapp %+v", tapp)
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

	c.setDefaultValue(newTapp)

	err = c.removeUnusedTemplate(newTapp)
	if err != nil {
		klog.Errorf("Failed to remove unused template for tapp %s: %v", util.GetTAppFullName(tapp), err)
		return err
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

func (c *Controller) setDefaultValue(tapp *tappv1.TApp) {
	// Set default value for UpdateStrategy
	if len(tapp.Spec.UpdateStrategy.Template) == 0 {
		tapp.Spec.UpdateStrategy.Template = tappv1.DefaultRollingUpdateTemplateName
	}
	if tapp.Spec.UpdateStrategy.MaxUnavailable == nil {
		maxUnavailable := int32(tappv1.DefaultMaxUnavailable)
		tapp.Spec.UpdateStrategy.MaxUnavailable = &maxUnavailable
	}
}

func (c *Controller) removeUnusedTemplate(tapp *tappv1.TApp) error {
	templateMap := make(map[string]bool, len(tapp.Spec.TemplatePool))
	for k := range tapp.Spec.TemplatePool {
		templateMap[k] = true
	}
	for _, template_used := range tapp.Spec.Templates {
		delete(templateMap, template_used)
	}
	rollingTemplate := getRollingTemplateKey(tapp)
	if len(rollingTemplate) != 0 {
		delete(templateMap, rollingTemplate)
	}
	for template_unused := range templateMap {
		delete(tapp.Spec.TemplatePool, template_unused)
	}
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

	desiredRunningPods, desiredCompletedPods := getDesiredInstance(tapp)
	podActions := c.getNextActionsForPods(tapp, desiredRunningPods, desiredCompletedPods, podMap)
	add, del, forceDel, update = c.transformPodActions(tapp, podActions, podMap, desiredRunningPods)

	return
}

// getAvailablePods returns available ready pod ids from desiredRunningPods. Those pods that are not
// in desiredRunningPods will be deleted.
func getAvailablePods(podMap map[string]*corev1.Pod, desiredRunningPods sets.String) (availablePods sets.String) {
	availablePods = make(sets.String)
	for _, id := range desiredRunningPods.List() {
		pod, exist := podMap[id]
		if !exist {
			continue
		}
		if isPodInActive(pod) || isUpdating(pod) {
			continue
		}

		if _, condition := GetPodCondition(&pod.Status, corev1.PodReady); condition != nil {
			if condition.Status == corev1.ConditionTrue {
				availablePods.Insert(id)
			}
		}
	}
	return
}

func (c *Controller) getNextActionsForPods(tapp *tappv1.TApp, desiredRunningPods sets.String,
	desiredCompletedPods sets.String, podMap map[string]*corev1.Pod) (podActions map[string]PodAction) {
	a1 := c.syncRunningPods(tapp, desiredRunningPods, podMap)
	a2 := c.syncCompletedPods(tapp, desiredCompletedPods, podMap)
	a3 := c.syncUnwantedPods(tapp, podMap)
	return mergePodActions(a1, a2, a3)
}

func (c *Controller) syncRunningPods(tapp *tappv1.TApp, desiredRunningPods sets.String,
	podMap map[string]*corev1.Pod) (podActions map[string]PodAction) {
	podActions = make(map[string]PodAction)
	for _, id := range desiredRunningPods.List() {
		if pod, ok := podMap[id]; !ok {
			// pod does not exist, should create it
			podActions[id] = createPod
		} else {
			if isPodDying(pod) {
				// Pod is being deleted, check whether we need delete it forcefully.
				podActions[id] = deletePod
			} else if isPodCompleted(pod) {
				// Pod is completed, migrate pod or just leave it be
				if migrate, err := shouldPodMigrate(tapp, pod, id); err != nil {
					klog.Errorf("Failed to determine whether needs migrate pod %s: %v", getPodFullName(pod), err)
				} else if migrate {
					klog.V(4).Infof("Will migrate pod %s", getPodFullName(pod))
					// Delete it, then create it in the next sync.
					podActions[id] = deletePod
				} else {
					klog.V(6).Infof("Skip migrating pod %s, status:%s", getPodFullName(pod), pod.Status.Phase)
				}
			} else if c.isTemplateHashChanged(tapp, id, pod) {
				// If template hash changes, it means some thing in pod spec got updated.
				// If image is updated, we only need restart corresponding container, otherwise
				// we need recreate the pod because k8s does not support restarting it.
				if !c.isUniqHashChanged(tapp, id, pod) {
					// Update pod directly
					podActions[id] = updatePod
				} else {
					// Recreate pod: delete it firstly, then create it.
					klog.V(4).Infof("Will recreate pod %s", getPodFullName(pod))
					podActions[id] = recreatePod
				}
			}
		}
	}
	return
}

func (c *Controller) syncCompletedPods(tapp *tappv1.TApp, desiredCompletedPods sets.String,
	podMap map[string]*corev1.Pod) (podActions map[string]PodAction) {
	podActions = make(map[string]PodAction)
	// Sync desired completed pods
	for _, id := range desiredCompletedPods.List() {
		if _, ok := podMap[id]; ok {
			podActions[id] = deletePod
		}
	}
	return
}

func (c *Controller) syncUnwantedPods(tapp *tappv1.TApp,
	podMap map[string]*corev1.Pod) (podActions map[string]PodAction) {
	podActions = make(map[string]PodAction)
	// Delete pods that exceed replica
	for id := range podMap {
		if idInt, err := strconv.Atoi(id); err == nil {
			if idInt >= int(tapp.Spec.Replicas) {
				podActions[id] = deletePod
			}
		}
	}
	return
}

func mergePodActions(actions ...map[string]PodAction) map[string]PodAction {
	result := make(map[string]PodAction)
	for _, a := range actions {
		for k, v := range a {
			result[k] = v
		}
	}

	return result
}

func (c *Controller) transformPodActions(tapp *tappv1.TApp, podActions map[string]PodAction, podMap map[string]*corev1.Pod,
	desiredRunningPods sets.String) (add, del, forceDel, update []*Instance) {
	var rollingUpdateIds []string
	availablePods := getAvailablePods(podMap, desiredRunningPods)
	// Delete pods
	for p, a := range podActions {
		pod := podMap[p]
		switch a {
		case deletePod:
			ins, err := newInstanceWithPod(tapp, pod)
			if err != nil {
				continue
			}
			if c.needForceDelete(tapp, podMap[p]) {
				forceDel = append(forceDel, ins)
			} else if !isPodDying(pod) {
				del = append(del, ins)
			}
			availablePods.Delete(p)
			break
		case createPod:
			if instance, err := newInstance(tapp, p); err == nil {
				add = append(add, instance)
			}
			break
		case updatePod:
			if !isInRollingUpdate(tapp, p) {
				if instance, err := newInstance(tapp, p); err == nil {
					update = append(update, instance)
					availablePods.Delete(p)
				}
			} else {
				rollingUpdateIds = append(rollingUpdateIds, p)
			}
			break
		case recreatePod:
			if !isInRollingUpdate(tapp, p) {
				if instance, err := newInstanceWithPod(tapp, pod); err == nil {
					del = append(del, instance)
					availablePods.Delete(p)
				}
			} else {
				rollingUpdateIds = append(rollingUpdateIds, p)
			}
			break
		default:
			klog.Errorf("Unknown pod action %v for pod %v", a, getPodFullName(pod))
		}
	}

	maxUnavailable := tappv1.DefaultMaxUnavailable
	if tapp.Spec.UpdateStrategy.MaxUnavailable != nil {
		maxUnavailable = int(*tapp.Spec.UpdateStrategy.MaxUnavailable)
	}
	minAvailablePods := desiredRunningPods.Len() - maxUnavailable

	// First sort ids.
	sort.Slice(rollingUpdateIds, func(i, j int) bool {
		id1, _ := strconv.Atoi(rollingUpdateIds[i])
		id2, _ := strconv.Atoi(rollingUpdateIds[j])
		return id1 < id2
	})
	// Rolling update
	for _, p := range rollingUpdateIds {
		pod := podMap[p]
		action := podActions[p]
		if availablePods.Has(p) && len(availablePods) <= minAvailablePods {
			klog.V(3).Infof("Skip %v pod %v because available pods are not enough: %v(current) vs %v(min)",
				action, getPodFullName(pod), len(availablePods), minAvailablePods)
			continue
		}
		switch action {
		case updatePod:
			if instance, err := newInstance(tapp, p); err == nil {
				update = append(update, instance)
				availablePods.Delete(p)
			}
			break
		case recreatePod:
			if instance, err := newInstanceWithPod(tapp, pod); err == nil {
				del = append(del, instance)
				availablePods.Delete(p)
			}
			break
		default:
			klog.Errorf("Unknown pod action %v for pod %v", action, getPodFullName(pod))
		}
	}

	return
}

func isInRollingUpdate(tapp *tappv1.TApp, podId string) bool {
	rollingTemplateName := getRollingTemplateKey(tapp)
	templateName := getPodTemplateName(tapp.Spec.Templates, podId)
	return templateName == rollingTemplateName
}

func (c *Controller) needForceDelete(tapp *tappv1.TApp, pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
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
			if tapp.Spec.Statuses[id] == tappv1.InstanceKilled {
				statuses[id] = tappv1.InstanceKilled
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

func (c *Controller) syncTApp(tapp *tappv1.TApp, pods []*corev1.Pod) {
	klog.V(4).Infof("Syncing tapp %s with %d pods", util.GetTAppFullName(tapp), len(pods))
	add, del, forceDel, update := c.instanceToSync(tapp, pods)
	c.syncPodConditions(pods, append(del, update...))
	c.syncer.SyncInstances(add, del, forceDel, update)
}

func (c *Controller) syncPodConditions(allPods []*corev1.Pod, notReadyInstances []*Instance) {
	var wg sync.WaitGroup
	wg.Add(len(allPods))

	notReadyPods := make(sets.String)
	for _, ins := range notReadyInstances {
		notReadyPods.Insert(getPodFullName(ins.pod))
	}
	for _, pod := range allPods {
		if notReadyPods.Has(getPodFullName(pod)) {
			go func(pod *corev1.Pod) {
				defer wg.Done()
				klog.V(4).Infof("Set pod %v %v to false because pod will not be ready",
					getPodFullName(pod), tappv1.InPlaceUpdateReady)
				setInPlaceUpdateCondition(c.kubeclient, pod, corev1.ConditionFalse)
			}(pod)
		} else {
			go func(pod *corev1.Pod) {
				defer wg.Done()
				c.syncInPlaceUpdateCondition(pod)
			}(pod)
		}
	}
	wg.Wait()
}

func (c *Controller) syncInPlaceUpdateCondition(pod *corev1.Pod) {
	status := corev1.ConditionTrue
	if pod.Status.Phase == corev1.PodRunning && isUpdating(pod) {
		status = corev1.ConditionFalse
	}
	setInPlaceUpdateCondition(c.kubeclient, pod, status)
}

func setInPlaceUpdateCondition(kubeclient kubernetes.Interface, pod *corev1.Pod, status corev1.ConditionStatus) {
	if kubeclient == nil {
		klog.V(4).Infof("Skip setInPlaceUpdateCondition because kubeclient is nil")
		return
	}
	for r := 0; r < statusUpdateRetries; r++ {
		needUpdate := true
		found := false
		for i, c := range pod.Status.Conditions {
			if c.Type == tappv1.InPlaceUpdateReady {
				found = true
				if c.Status != status {
					pod.Status.Conditions[i].Status = status
					pod.Status.Conditions[i].LastTransitionTime = metav1.Now()
				} else {
					needUpdate = false
				}
				break
			}
		}
		if !found {
			pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
				Type:               tappv1.InPlaceUpdateReady,
				Status:             status,
				LastTransitionTime: metav1.Now(),
			})
		}

		if !needUpdate {
			return
		}

		// If status is False, we also need to update Ready condition to False immediately
		if status == corev1.ConditionFalse {
			for i, c := range pod.Status.Conditions {
				if c.Type == corev1.PodReady {
					if c.Status != corev1.ConditionFalse {
						pod.Status.Conditions[i].Status = corev1.ConditionFalse
						pod.Status.Conditions[i].LastTransitionTime = metav1.Now()
					}
					break
				}
			}
		}

		klog.V(3).Infof("Update pod %v %v condition to %v", getPodFullName(pod),
			tappv1.InPlaceUpdateReady, status)
		if _, err := kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(pod); err != nil && errors.IsConflict(err) {
			klog.Errorf("Conflict to update pod %v condition, retrying", getPodFullName(pod))
			newPod, err := kubeclient.CoreV1().Pods(pod.Namespace).Get(pod.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get pod %v: %v, retrying...", getPodFullName(pod), err)
			} else {
				pod = newPod
			}
		} else {
			if err != nil {
				klog.Errorf("Failed to update pod %v %v condition to %v: %v", getPodFullName(pod),
					tappv1.InPlaceUpdateReady, status, err)
			}
			return
		}
	}
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
			klog.V(5).Infof("Pod %s/%s is updating: %v(expected) vs %v(got), imageId: %v",
				pod.Namespace, pod.Name, expectedImage, status.Image, status.ImageID)
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
