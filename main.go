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

package main

import (
	"context"
	"flag"
	"os"
	"time"

	"tkestack.io/tapp/pkg/admission"
	clientset "tkestack.io/tapp/pkg/client/clientset/versioned"
	informers "tkestack.io/tapp/pkg/client/informers/externalversions"
	"tkestack.io/tapp/pkg/tapp"
	"tkestack.io/tapp/pkg/version/verflag"

	"github.com/spf13/pflag"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/logs"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/client/leaderelectionconfig"
)

var (
	masterURL  string
	kubeconfig string
	createCRD  bool
	// If true, delete instance pod after app finishes, otherwise delete pod once pod finishes.
	deletePodAfterAppFinish bool
	// kubeAPIQPS is the QPS to use while talking with kubernetes apiserver.
	kubeAPIQPS float32
	// kubeAPIBurst is the burst to use while talking with kubernetes apiserver.
	kubeAPIBurst int
	// TApp sync worker number
	worker int

	// Admission related config
	registerAdmission bool
	tlsCAfile         string
	tlsCertFile       string
	tlsKeyFile        string
	listenAddress     string
	// namespace to deploy tapp controller
	namespace string

	// leaderElection defines the configuration of leader election client.
	// The default value refers k8s.io/component-base/config/v1alpha1/defaults.go
	leaderElection = componentbaseconfig.LeaderElectionConfiguration{
		LeaderElect:   false,
		LeaseDuration: metav1.Duration{Duration: 15 * time.Second},
		RenewDeadline: metav1.Duration{Duration: 10 * time.Second},
		RetryPeriod:   metav1.Duration{Duration: 2 * time.Second},
		ResourceLock:  "endpoints",
	}
)

const (
	defaultWorkerNumber = 5
)

func main() {
	flag.CommandLine.Parse([]string{})

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}
	cfg.QPS = kubeAPIQPS
	cfg.Burst = kubeAPIBurst

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	tappClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}

	extensionsClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error instantiating apiextensions client: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	tappInformerFactory := informers.NewSharedInformerFactory(tappClient, time.Second*30)

	controller := tapp.NewController(kubeClient, tappClient, kubeInformerFactory, tappInformerFactory)
	run := func(ctx context.Context) {
		stop := ctx.Done()
		if createCRD {
			wait.PollImmediateUntil(time.Second*5, func() (bool, error) { return tapp.EnsureCRDCreated(extensionsClient) }, stop)
		}

		if registerAdmission {
			wait.PollImmediateUntil(time.Second*5, func() (bool, error) {
				return admission.Register(kubeClient, namespace, tlsCAfile)
			}, stop)
			server, err := admission.NewServer(listenAddress, tlsCertFile, tlsKeyFile)
			if err != nil {
				klog.Fatalf("Error new admission server: %v", err)
			}
			go server.Run(stop)
		}

		go kubeInformerFactory.Start(stop)
		go tappInformerFactory.Start(stop)

		if worker == 0 {
			worker = defaultWorkerNumber
		}

		tapp.SetDeletePodAfterAppFinish(deletePodAfterAppFinish)

		if err = controller.Run(worker, stop); err != nil {
			klog.Fatalf("Error running controller: %s", err.Error())
		}
	}

	if !leaderElection.LeaderElect {
		run(context.TODO())
		panic("unreachable")
	}

	id, err := os.Hostname()
	if err != nil {
		klog.Fatalf("Failed to get hostname: %s", err.Error())
	}

	leaderElectionClient := kubernetes.NewForConfigOrDie(restclient.AddUserAgent(cfg, "leader-election"))
	rl, err := resourcelock.New(leaderElection.ResourceLock,
		"kube-system",
		"tapp-controller",
		leaderElectionClient.CoreV1(),
		leaderElectionClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: controller.GetEventRecorder(),
		})
	if err != nil {
		klog.Fatalf("error creating lock: %v", err)
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: leaderElection.LeaseDuration.Duration,
		RenewDeadline: leaderElection.RenewDeadline.Duration,
		RetryPeriod:   leaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				klog.Fatalf("leaderelection lost")
			},
		},
	})
	panic("unreachable")
}

func init() {
	addFlags(pflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	verflag.PrintAndExitIfRequested()
}

func addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	fs.StringVar(&masterURL, "master", "",
		"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	fs.BoolVar(&createCRD, "create-crd", true, "Create TApp CRD if it does not exist")
	fs.BoolVar(&deletePodAfterAppFinish, "delete-pod-after-app-finish", false,
		"If true, delete instance pod after app finishes, otherwise delete pod once pod finishes")
	fs.Float32Var(&kubeAPIQPS, "kube-api-qps", kubeAPIQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&kubeAPIBurst, "kube-api-burst", kubeAPIBurst, "Burst to use while talking with kubernetes apiserver")
	fs.IntVar(&worker, "worker", worker, "TApp sync worker number, default: 5")

	// Admission related
	fs.BoolVar(&registerAdmission, "register-admission", false, "Register admission for tapp controller")
	fs.StringVar(&tlsCAfile, "tlsCAFile", "/etc/certs/ca.crt", "File containing the x509 CA for HTTPS")
	fs.StringVar(&listenAddress, "listen-address", ":8443", "The address to listen on for HTTP requests.")
	fs.StringVar(&tlsCertFile, "tlsCertFile", "/etc/certs/tls.crt", "File containing the x509 Certificate for HTTPS.")
	fs.StringVar(&tlsKeyFile, "tlsKeyFile", "/etc/certs/tls.key", "File containing the x509 private key to for HTTPS.")
	fs.StringVar(&namespace, "namespace", "kube-system", "Namespace to deploy tapp controller")

	leaderelectionconfig.BindFlags(&leaderElection, fs)
}
