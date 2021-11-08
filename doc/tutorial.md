# Tapp tutorial

Table of Contents
=================

   * [Tapp tutorial](#tapp-tutorial)
      * [1. Tapp's struct](#1-tapps-struct)
      * [2. Common operations](#2-common-operations)
         * [2.1 Create a new tapp](#21-create-a-new-app)
            * [2.1.1 Create tapp with same template](#211-create-tapp-with-same-template)
            * [2.1.2 Create tapp with different templates](#212-create-tapp-with-different-templates)
         * [2.2 Get tapp](#22-get-tapp)
         * [2.3 Update tapp](#23-update-tapp)
            * [2.3.1 Update specified pod](#231-update-specified-pod)
            * [2.3.2 Force update](#232-force-update)
            * [2.3.3 Rolling update](#233-rolling-update)
         * [2.4 Kill specified pod](#24-kill-specified-pod)
         * [2.5 Scale up tapp](#25-scale-up-tapp)
         * [2.6 Delete tapp](#26-delete-tapp)
         * [2.7 Others](#27-others)
            * [2.7.1 Get stable, unique network identifiers like statefulset](#271-get-stable-unique-network-identifiers-like-statefulset)
            * [2.7.2 Auto delete template when it is unused](#272-auto-delete-template-when-it-is-unused)

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

	// UpdateStrategy indicates the TappUpdateStrategy that will be
	// employed to update Pods in the TApp
	UpdateStrategy TAppUpdateStrategy `json:"updateStrategy,omitempty"`

	// ForceDeletePod indicates whether force delete pods when it is being deleted because of NodeLost.
	// Default values is false.
	ForceDeletePod bool `json:"forceDeletePod,omitempty"`

	// AutoDeleteUnusedTemplate indicates whether auto delete templates when it is unused.
	// Default values is false.
	AutoDeleteUnusedTemplate bool `json:"autoDeleteUnusedTemplate,omitempty"`

	// NeverMigrate indicates whether to migrate pods. If it is true, pods will never be migrated to
	// other nodes, otherwise it depends on other conditions(e.g. pod restart policy).
	NeverMigrate bool `json:"neverMigrate,omitempty"`

	// RetainFinishedPod indicates whether to delete pod after all pods are failed or success
	// Default values is false.
	RetainFinishedPod bool `json:"retainFinishedPod,omitempty"`

	// volumeClaimTemplates is a list of claims that pods are allowed to reference.
	// The TApp controller is responsible for mapping network identities to
	// claims in a way that maintains the identity of a pod. Every claim in
	// this list must have at least one matching (by name) volumeMount in one
	// container in the template. A claim in this list takes precedence over
	// any volumes in the template, with the same name.
	// TODO: Define the behavior if a claim already exists with the same name.
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

	// ServiceName is the name of the service that governs this TApp.
	// This service must exist before the TApp, and is responsible for
	// the network identity of the set. Pods get DNS/hostnames that follow the
	// pattern: pod-specific-string.serviceName.default.svc.cluster.local
	// where "pod-specific-string" is managed by the TApp controller.
	ServiceName string `json:"serviceName,omitempty"`

	//DefaultTemplateName is the default template name for scale
	DefaultTemplateName string `json:"defaultTemplateName"`
}

// support rolling update and force update now
type TAppUpdateStrategy struct {
	// Template is the rolling update template name
	Template string `json:"template,omitempty"`
	// MaxUnavailable is the max unavailable number when tapp is rolling update, default is 1.
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
	// Following fields are force update related configuration.
	ForceUpdate ForceUpdateStrategy `json:"forceUpdate,omitempty"`
}

type ForceUpdateStrategy struct {
	// MaxUnavailable is the max unavailable number when tapp is forced update, default is 100%.
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// TAppStatus represents the current state of a TApp.
type TAppStatus struct {
	// most recent generation observed by controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Replicas is the number of actual replicas.
	Replicas int32 `json:"replicas"`
	
	// ReadyReplicas is the number of running replicas
	ReadyReplicas int32 `json:"readyReplicas"`

	// ScaleSelector is a label for query over pods that should match the replica count used by HPA.
	ScaleLabelSelector string `json:"scaleLabelSelector,omitempty"`

	// AppStatus describe the current TApp state
	AppStatus AppStatus `json:"appStatus,omitempty"`

	// Statues stores actual instanceID --> InstanceStatus
	Statuses map[string]InstanceStatus `json:"statuses,omitempty"`
}
```

## 2. Common operations

### 2.1 Create a new tapp

#### 2.1.1 Create tapp with same template

The following is an example of a basic TAPP. It creates three nginx Pods:
  ```yaml
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
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

In this example:
- A TApp named example-tapp is created, indicated by the `metadata.name` field.
- The TApp creates three replicated Pods from the `spec.template`, indicated by the `spec.replicas` field.
- These three pods have unique identity and will be named as example-tapp-0, example-tapp-1, example-tapp-2


##### Pod Ordinal Index
For a Tapp with N replicas, each Pod in the Tapp will be assigned an integer ordinal, from 0 up through N-1, that is unique over the Set.


#### 2.1.2 Create tapp with different templates

In the previous example, all the pods are created from the same template in `spec.template` field. TApp also support creates pods with different templates with following steps.
- Firstly,create templates in `spec.templatePools`, and the name of template is used as the key. In the following example, two pod templates named `"test1"` and `"test2"` are set. 
- Then, specify the template pod will use in `spec.templates`,  e.g. `"1":"test1"` in following example. `"1"` is the Ordinal Index of the pod, `test1` is the name of template that pod will use. If not specified in `spec.templates`, other pods will use default template
- You can set the default template in `spec.templatPools` with `spec.DefaultTemplateName` , otherwise default template will be in `spec.template`. In the following example, pod 0 and pod 2 will use the defaultTemplate `"test2"`
  
```yaml
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
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
      "test1":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.8.0
      "test2":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
            - name: nginx
              image: nginx:1.8.1
    templates:
      "1": "test1"
    defaultTemplateName: "test2"
```

### 2.2 Get tapp

you can use the following commands to get Tapp

```
$ kubectl descirbe tapp XXX
```

or

```
$ kubectl get tapp XXX
```

### 2.3 Update tapp

Tapp controller will do **in-place update** for pod if only containers' images are updated, otherwise it will delete pods and recreate them.

#### 2.3.1 Update specified pod

  Create a new template in `spec.templatePool` and specify template to pod using `spec.templates`, or update existing template in `spec.templatePool`. These pods will be updated concurrently.

  ```yaml
  $ # edit tapp.yaml, update pod 1 to use template test3 .
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
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
            image: nginx:1.8.1
      "test3":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
    templates:
      "1": "test3"
    defaultTemplateName: "test2"
  $ kubect apply -f ./tapp.yaml
  ```
In the previous example , we add a new template named "test3" in `spec.templatePool` and specify pod `1` to use it in `spec.templates`, so the pod `example-tapp-1` will be updated.

TApp supports two different update strategy using `spec.UpdateStrategy`: Force update and Rolling update.

#### 2.3.2 Force update

TApp ensures that only a certain number of pods are down while they are being updated using `spec.UpdateStrategy.ForceUpdate.MaxUnavailable`. By default, the `ForceUpdate.MaxUnavailable` is 100% which means all the pod can be down.

The `MaxUnavailable` can be a number like 1 or a percentage string like "50%".

 ```yaml
  $ # edit tapp.yaml, update pod 2 and 3 to use template test3 .
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
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
      "test1":
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
          - name: nginx
            image: nginx:1.8.0
      "test3":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
    templates:
      "1": "test1"
    defaultTemplateName: "test3"
    updateStrategy:
      forceUpdate:
         maxUnavailable: 50%
  $ kubect apply -f ./tapp.yaml
  ```

In the previous example, we add a new template named "test3" in `spec.templatePool` and replace the default template from "test2" to "test3", So the pod 2 and pod 3 will be updated. We use the force update strategy with 50% maxUnavailable which means at most 50% of the 3 running pods will be updating at the same time. In fact, we can find pod 2 and pod 3 will not be updating at the same time.

#### 2.3.3 Rolling update

Use `Spec.UpdateStrategy.Template` to specify the rolling update template, add `Spec.UpdateStrategy.MaxUnavailable` to specify max unavailable pods(default: 1).

  ```yaml
  $ # edit tapp.yaml, update pod 1 to use template test2 and rolling update pod 2,3 to use template test3
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
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
      "test3":
         metadata:
            labels:
               app: example-tapp
         spec:
            containers:
               - name: nginx
                 image: nginx:1.7.9
    templates:
      "1": "test2"
    defaultTemplateName: "test3"
    updateStrategy:
      template: test3
      maxUnavailable: 1
  $ kubect apply -f ./tapp.yaml
  ```

In the previous example, pod 1 will be updated to template 'test2' while pod 2 and 3 will be updated to template 'test3'.  we set the template "test3" as the rolling update template with max unavailable 1 which means at most 1 pod in pod2 and 3 will be updating at the same time.

### 2.4 Kill specified pod

Specify pod's status in `spec.statuses`, and tapp controller will reconcile it, e.g. if `spec.statuses` is `"1":"Killed"`, tapp controller will kill pod 1.

```yaml
$ # edit tapp.yaml, kill pod 1.
$ cat tapp.yaml
apiVersion: apps.tkestack.io/v1
kind: TApp
metadata:
  name: example-tapp
  labels:
    app: example-tapp
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

If you want to run pod 1 again, just delete `spec.statuses`.

### 2.5 Scale up tapp

If you want to scale up tapp use default template just increased value of `spec.replicas`, otherwise you'll need specify which template pods will use in `spec.templates`. And `kubectl scale` also works for tapp. 

You can set the default template in "spec.templatePool" with "spec.DefaultTemplateName" , otherwise  default template will be in `spec.template`

e.g.

```
$ kubectl scale --replicas=3 --resource-version='5892' tapp/nginx
```

### 2.6 Delete tapp

```
$ kubectl delete tapp XXX
```

### 2.7 Others

Tapp also supports other features, e.g. HPA, volume templates, they are similar to workloads in kubernetes.

#### 2.7.1 Get stable, unique network identifiers like statefulset

Create a Headless Service and set `spec.serviceName` in TApp, then each pod will get a stable, matching DNS subdomain, taking the form: $(podname).$(namespace).svc.cluster.local,where "cluster.local" is the cluster domain.

```yaml
  $ cat tapp.yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: nginx
    labels:
      app: example-tapp
  spec:
    ports:
    - port: 80
      name: web
    clusterIP: None
    selector:
      app: example-tapp
---
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
  spec:
    replicas: 3
    serviceName: "nginx"
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:1.7.9
```

#### 2.7.2 Auto delete template when it is unused

If you want to delete unused templates in `spec.templatePool` to make the tapp concise, set `spec.autoDeleteUnusedTemplate` with true.
e.g. in the follow yaml, template "test2" will be deleted while other template will be retained.
The delete operation will be irreversible and make sure you will not use these template again.
 
```yaml
  $ cat tapp.yaml
  apiVersion: apps.tkestack.io/v1
  kind: TApp
  metadata:
    name: example-tapp
    labels:
      app: example-tapp
  spec:
    replicas: 3
    autoDeleteUnusedTemplate: true
    template:
      metadata:
        labels:
          app: example-tapp
      spec:
        containers:
        - name: nginx
          image: nginx:1.7.9
    templatePool:
      "test1":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.9
      "test2":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.8
      "test3":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.7
      "test4":
        metadata:
          labels:
            app: example-tapp
        spec:
          containers:
          - name: nginx
            image: nginx:1.7.7
    templates:
      "1": "test1"
    updateStrategy:
      template: "test3"
      maxUnavailable: 1
    DefaultTemplateName: "test4"
```


