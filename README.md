# TAPP

TAPP is a CustomResourceDefinition(CRD) based app kind, it contains most features of kubernetes `deployment` and `statefulset`, and it is easy for users to run legacy applications on Kubernetes. Nowadays, many customers want to adopt Kubernetes, and migrate their legacy applications to Kubernetes. However they could not use Kubernetes’ workloads(e.g. `deployment`, `statefulset`) directly, and it will take a lot of effort to transform those applications to microservices. TAPP could solve these problems. 

## Features

* Support unique index for each instance(same as `statefulset`)

* Support operating(start/stop/upgrade) on specific instances(pods)

  It is more suitable for traditional operation and maintenance, e.g. when administor want to stop one machine, he could just stop instances on that machine, and do not affect instances on other machines.

* Support in place update for instances

  While many stateless workloads are designed to withstand such a disruption, some are more sensitive, a Pod restart is a serious disruption, resulting in lower availability or higher cost of running.

* Support multi versions of instances

  Instances use different images or different config.

* Support Horizontal Pod Autoscaler(HPA) according to a lot kinds of metrics(e.g. CPU, memory, custom metrics)

* Support rolling update, rolling back

## Usage

Find more usage at [tutorial.md](doc/tutorial.md).

## Build

``` sh
$ make build
```

## Run

```sh
# assumes you have a working kubeconfig, not required if operating in-cluster
$ bin/tapp-controller --master=127.0.0.1:8080    // Assume 127.0.0.1:8080 is k8s master ip:port
or
$ bin/tapp-controller --kubeconfig=$HOME/.kube/config

# create a custom resource of type TApp
$ kubectl create -f artifacts/examples/example-tapp.yaml

# check pods created through the custom resource
$ kubectl get pods
```

## Cleanup

You can clean up the created CustomResourceDefinition with:

    $ kubectl delete crd tapps.apps.tkestack.io
