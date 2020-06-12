# Tapp tutorial

Table of Contents
=================

   * [Tapp tutorial](#tapp-tutorial)
      * [1. Tapp's struct](#1-tapps-struct)
      * [2. Common operations](#2-common-operations)
         * [2.1 Create a new app](#21-create-a-new-app)
         * [2.2 Get tapp](#22-get-tapp)
         * [2.3 Update tapp](#23-update-tapp)
         * [2.4 Kill specified pod](#24-kill-specified-pod)
         * [2.5 Scale up tapp](#25-scale-up-tapp)
         * [2.6 Delete tapp](#26-delete-tapp)
         * [2.7 Others](#27-others)

## 1. Tapp's struct

The struct for tapp:

```go
// TApp represents a set of pods with consistent identities.
type TApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired identities of pods in this tapp.
	Spec TAppSpec `json:"spec,omitempty"`

	// Status is the current status of pods in this TApp. This data
	// may be out of date by some window of time.
	Status TAppStatus `json:"status,omitempty"`
}

// A TAppSpec is the specification of a TApp.
type TAppSpec struct {
	// Replicas is the desired number of replicas of the given Template.
	// These are replicas in the sense that they are instantiations of the
	// same Template, but individual replicas also have a consistent identity.
	Replicas int32 `json:"replicas,omitempty"`

	// Selector is a label query over pods that should match the replica count.
	// If empty, defaulted to labels on the pod template.
	// More info: http://releases.k8s.io/release-1.4/docs/user-guide/labels.md#label-selectors
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Template is the object that describes the pod that will be initial created/default scaled
	// it should be added to TemplatePool
	Template corev1.PodTemplateSpec `json:"template"`

	// TemplatePool stores a map whose key is template name and value is podTemplate
	TemplatePool map[string]corev1.PodTemplateSpec `json:"templatePool,omitempty"`

	// Statuses stores desired instance status instanceID --> desiredStatus
	Statuses map[string]InstanceStatus `json:"statuses,omitempty"`

	// Templates stores instanceID --> template name
	Templates map[string]string `json:"templates,omitempty"`

	// UpdateStrategy indicates the StatefulSetUpdateStrategy that will be
	// employed to update Pods in the TApp
	UpdateStrategy TAppUpdateStrategy `json:"updateStrategy,omitempty"`

	// ForceDeletePod indicates whether force delete pods when it is being deleted because of NodeLost.
	// Default values is false.
	ForceDeletePod bool `json:"forceDeletePod,omitempty"`

	// volumeClaimTemplates is a list of claims that pods are allowed to reference.
	// The StatefulSet controller is responsible for mapping network identities to
	// claims in a way that maintains the identity of a pod. Every claim in
	// this list must have at least one matching (by name) volumeMount in one
	// container in the template. A claim in this list takes precedence over
	// any volumes in the template, with the same name.
	// TODO: Define the behavior if a claim already exists with the same name.
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`
}

// Only support rolling update now
type TAppUpdateStrategy struct {
	// Template is the rolling update template name
	Template string `json:"template,omitempty"`
	// MaxUnavailable is the max unavailable number when tapp is rolling update, default is 1.
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

// TAppStatus represents the current state of a TApp.
type TAppStatus struct {
	// most recent generation observed by controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Replicas is the number of actual replicas.
	Replicas int32 `json:"replicas"`

	// ScaleSelector is a label for query over pods that should match the replica count used by HPA.
	ScaleLabelSelector string `json:"scaleLabelSelector,omitempty"`

	// AppStatus describe the current TApp state
	AppStatus AppStatus `json:"appStatus,omitempty"`

	// Statues stores actual instanceID --> InstanceStatus
	Statuses map[string]InstanceStatus `json:"statuses,omitempty"`
}
```

## 2. Common operations

### 2.1 Create a new app

* Create app with same template

  ```yaml
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
  spec:
    replicas: 3
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:1.7.9
  $ kubect apply -f ./tapp.yaml  
  ```

* Create app with different templates

  Firstly, create template in`spec.templatPools`, then speicify the template pod will use in `spec.templates`, e.g. `"1":"test"` means creating pod 1 using template `test`, use default template in `spec.template`  if not specified.

  ```yaml
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
  spec:
    replicas: 3
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:1.7.9
    templatePool:
      "test":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
    templates:
      "1": "test"
  ```

### 2.2 Get tapp

```
$ kubectl descirbe tapp XXX
```

or 

```
$ kubectl get tapp XXX
```

### 2.3 Update tapp

Tapp controller will do **in-place update** for pod if only containers' images are updated, otherwise it will delete pods and recreate them.

* Update specified pod

  Create a new template in `spec.templatePool` and specify template to pod using `spec.templates`, or update existing template in `spec.templatePool`. These pods will be updated concurrently.

  ```yaml
  $ # edit tapp.yaml, update pod 1 to use template test2.
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
  spec:
    replicas: 3
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:latest
    templatePool:
      "test2":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
    templates:
       "1": "test2"
  $ kubect apply -f ./tapp.yaml
  ```

* Rolling update

  Create a new template in `spec.templatePool` and specify template to pod using `spec.templates`, or update existing template in `spec.templatePool`. And use `Spec.UpdateStrategy.Template` to specify the rolling update template, add `Spec.UpdateStrategy.MaxUnavailable` to specify max unavailable pods(default: 1).

  ```yaml
  $ # edit tapp.yaml, update pod 1 to use template test2.
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
  spec:
    replicas: 3
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:latest
    templatePool:
      "test2":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
    templates:
      "1": "test2"
      "2": "test2"
    updateStrategy:
      template: test2
      maxUnavailable: 1
  $ kubect apply -f ./tapp.yaml
  ```

### 2.4 Kill specified pod

Specify pod's status in `spec.statuses`, and tapp controller will reconcile it, e.g. if `spec.statuses` is `"1":"Killed"`, tapp controller will kill pod 1.

```yaml
$ # edit tapp.yaml, kill pod 1.
$ cat tapp.yaml
apiVersion: apps.tkestack.io/v1
kind: TApp
metadata:
  name: example-tapp
spec:
  replicas: 3
  template:
    metadata:
      labels:
        app: example-tapp
    spec:
      containers:
      - name: nginx
        image: nginx:latest
  templatePool:
    "test2":
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:1.7.9
  templates:
    "1": "test2"
  statuses:
    "1": "Killed"
$ kubect apply -f ./tapp.yaml
```

If you want to run pod 1 again, just delete `spec.status`.

### 2.5 Scale up tapp

If you want to scale up tapp use default template `spec.template`, just increased value of `spec.replicas`, otherwise you'll need specify which template pods will use in `spec.templates`. And `kubectl scale` also works for tapp.

e.g.

```
$ kubectl scale --replicas=3 --resource-version='5892' tapp/nginx
```

### 2.6 Delete tapp

```
$ kubectl delete tapp XXX
```

### 2.7 Others

Tapp also supports other features, e.g. HPA, volume templates, they are simlimar to workloads in kubernetes.