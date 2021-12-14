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

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"tkestack.io/tapp/pkg/apis/tappcontroller"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	validatingWebhookConfiguration = "tapp-admission"
)

var (
	validatePath                                                = "/validate/tapp"
	failPolicy   admissionregistrationv1beta1.FailurePolicyType = "Fail"
)

// Register registers the validatingWebhookConfiguration to kube-apiserver
// Note: always return err as nil, it will be used by wait.PollUntil().
func Register(clientset *kubernetes.Clientset, namespace string, caFile string) (bool, error) {
	klog.Infof("Starting to register validatingWebhookConfiguration")
	defer func() {
		klog.Infof("Finished registering validatingWebhookConfiguration")
	}()

	caCert, err := ioutil.ReadFile(caFile)
	if err != nil {
		klog.Errorf("Failed to read certificate authority from %s: %v", caFile, err)
		return false, nil
	}

	webhookConfig := &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: validatingWebhookConfiguration,
		},
		Webhooks: []admissionregistrationv1beta1.ValidatingWebhook{
			{
				Name: fmt.Sprintf("tapp-controller.%s.svc", namespace),
				Rules: []admissionregistrationv1beta1.RuleWithOperations{{
					Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create,
						admissionregistrationv1beta1.Update},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{tappcontroller.GroupName},
						APIVersions: []string{"v1"},
						Resources:   []string{"tapps"},
					},
				}},
				FailurePolicy: &failPolicy,
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Namespace: namespace,
						Name:      "tapp-controller",
						Path:      &validatePath,
					},
					CABundle: caCert,
				},
			},
		},
	}

	client := clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	if present, err := client.Get(context.TODO(), validatingWebhookConfiguration, metav1.GetOptions{}); err == nil {
		if !reflect.DeepEqual(present.Webhooks, webhookConfig.Webhooks) {
			klog.V(1).Infof("Update validationWebhookConfiguration from %+v to %+v", present, webhookConfig)
			webhookConfig.ResourceVersion = present.ResourceVersion
			if _, err := client.Update(context.TODO(), webhookConfig, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("Failed to update validationWebhookConfiguration: %v", err)
				return false, nil
			}
		}
	} else {
		if _, err := client.Create(context.TODO(), webhookConfig, metav1.CreateOptions{}); err != nil {
			klog.Errorf("Failed to create validatingWebhookConfiguration: %v", err)
			return false, nil
		}
	}

	return true, nil
}

// Server will start a https server for admitting.
type Server struct {
	listenAddress string
	certFile      string
	keyFile       string
}

// NewServer create a new Server for admitting.
func NewServer(listenAddress, certFile, keyFile string) (*Server, error) {
	server := &Server{
		listenAddress: listenAddress,
		certFile:      certFile,
		keyFile:       keyFile,
	}

	return server, nil
}

// Run starts informers, and listens for accepting request.
func (ws *Server) Run(stopCh <-chan struct{}) {
	mux := http.NewServeMux()
	mux.HandleFunc(validatePath, func(writer http.ResponseWriter, request *http.Request) {
		Serve(writer, request, validateHandler)
	})

	server := &http.Server{
		Addr:    ws.listenAddress,
		Handler: mux,
	}
	klog.Fatal(server.ListenAndServeTLS(ws.certFile, ws.keyFile))
}

func validateHandler(ar *admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Info("Admitting tapp")

	reviewResponse := &admissionv1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	var tapp tappv1.TApp
	raw := ar.Request.Object.Raw
	if err := json.Unmarshal(raw, &tapp); err != nil {
		klog.Errorf("Failed to unmarshal tapp from %s: %v", raw, err)
		return ToAdmissionResponse(err)
	}

	if err := validate(&tapp); err != nil {
		klog.Errorf("Failed to validate tapp %v: %v", tapp, err)
		return ToAdmissionResponse(err)
	}

	return reviewResponse
}
