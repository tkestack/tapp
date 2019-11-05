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
	"testing"

	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"
	"tkestack.io/tapp/pkg/testutil"
)

func TestInstanceIDName(t *testing.T) {
	tapp := testutil.CreateValidTApp(2)
	for i := 0; i < int(tapp.Spec.Replicas); i++ {
		instanceName := fmt.Sprintf("%v-%d", tapp.Name, i)
		instance, err := newInstance(tapp, fmt.Sprintf("%d", i))
		if err != nil {
			t.Fatalf("Failed to generate instance %v", err)
		}
		pod := instance.pod
		if pod.Name != instanceName || pod.Namespace != tapp.Namespace {
			t.Errorf("Wrong name identity, expected %v, real:%v", pod.Name, instanceName)
		}

		id, ok := pod.Labels[tappv1.TAppInstanceKey]
		instanceId := fmt.Sprintf("%d", i)
		if !ok || id != instanceId {
			t.Errorf("test on pod.lables[%s] failed, expected:%s, real:%s", tappv1.TAppInstanceKey, instanceId, id)
		}
	}
}
