/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
	tappcontrollerv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	versioned "tkestack.io/tapp/pkg/client/clientset/versioned"
	internalinterfaces "tkestack.io/tapp/pkg/client/informers/externalversions/internalinterfaces"
	v1 "tkestack.io/tapp/pkg/client/listers/tappcontroller/v1"
)

// TAppInformer provides access to a shared informer and lister for
// TApps.
type TAppInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.TAppLister
}

type tAppInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewTAppInformer constructs a new informer for TApp type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewTAppInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredTAppInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredTAppInformer constructs a new informer for TApp type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredTAppInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.TappcontrollerV1().TApps(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.TappcontrollerV1().TApps(namespace).Watch(context.TODO(), options)
			},
		},
		&tappcontrollerv1.TApp{},
		resyncPeriod,
		indexers,
	)
}

func (f *tAppInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredTAppInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *tAppInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&tappcontrollerv1.TApp{}, f.defaultInformer)
}

func (f *tAppInformer) Lister() v1.TAppLister {
	return v1.NewTAppLister(f.Informer().GetIndexer())
}
