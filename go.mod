module tkestack.io/tapp

go 1.12

require (
	github.com/evanphx/json-patch v4.5.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/groupcache v0.0.0-20191027212112-611e8accdfc9 // indirect
	github.com/googleapis/gnostic v0.3.1 // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/onsi/ginkgo v1.10.3 // indirect
	github.com/onsi/gomega v1.7.1 // indirect
	github.com/spf13/pflag v1.0.5
	golang.org/x/oauth2 v0.0.0-20191202225959-858c2ad4c8b6 // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	k8s.io/api v0.16.10
	k8s.io/apiextensions-apiserver v0.16.10
	k8s.io/apimachinery v0.16.10
	k8s.io/client-go v0.16.10
	k8s.io/component-base v0.16.10
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.16.10
	k8s.io/utils v0.0.0-20191114200735-6ca3b61696b6 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.16.10
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.10
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.10
	k8s.io/apiserver => k8s.io/apiserver v0.16.10
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.16.10
	k8s.io/client-go => k8s.io/client-go v0.16.10
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.16.10
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.16.10
	k8s.io/code-generator => k8s.io/code-generator v0.16.10
	k8s.io/component-base => k8s.io/component-base v0.16.10
	k8s.io/cri-api => k8s.io/cri-api v0.16.10
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.16.10
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.16.10
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.16.10
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.16.10
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.16.10
	k8s.io/kubectl => k8s.io/kubectl v0.16.10
	k8s.io/kubelet => k8s.io/kubelet v0.16.10
	k8s.io/kubernetes => k8s.io/kubernetes v1.17.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.16.10
	k8s.io/metrics => k8s.io/metrics v0.16.10
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.16.10
)
