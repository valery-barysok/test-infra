/*
Copyright 2020 gRPC authors.

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

package main

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/grpc/test-infra/config"
	"github.com/grpc/test-infra/kubehelpers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("WaitForReadyPods", func() {
	var fastDuration time.Duration
	var slowDuration time.Duration

	var irrelevantPod corev1.Pod
	var driverPod corev1.Pod
	var serverPod corev1.Pod
	var clientPod corev1.Pod

	BeforeEach(func() {
		fastDuration = 1 * time.Millisecond * timeMultiplier
		slowDuration = 100 * time.Millisecond * timeMultiplier

		irrelevantPod = corev1.Pod{}

		driverPod = newTestPod("driver")
		driverRunContainer := kubehelpers.ContainerForName(config.RunContainerName, driverPod.Spec.Containers)
		driverRunContainer.Ports = nil

		serverPod = newTestPod("server")
		serverPod.Status.PodIP = "127.0.0.2"

		clientPod = newTestPod("client")
		clientPod.Status.PodIP = "127.0.0.3"
	})

	It("returns successfully without args", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mock := &PodListerMock{
			PodList: &corev1.PodList{},
		}

		podAddresses, err := WaitForReadyPods(ctx, mock, []string{})
		Expect(err).ToNot(HaveOccurred())
		Expect(podAddresses).To(BeEmpty())
	})

	It("timeout reached when no matching pods are found", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{irrelevantPod},
			},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{"hello=anyone-out-there"})
		Expect(err).To(HaveOccurred())
	})

	It("timeout reached when only some matching pods are found", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{
					driverPod,
					clientPod,
					// note: missing 2nd client
				},
			},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{"role=driver", "role=client", "role=client"})
		Expect(err).To(HaveOccurred())
	})

	It("timeout reached when pod does not match all labels", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{driverPod},
			},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{"role=driver,loadtest=loadtest-1"})
		Expect(err).To(HaveOccurred())
	})

	It("timeout reached when not ready pod matches", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		driverPod.Status.ContainerStatuses[0].Ready = false

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{driverPod},
			},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{"role=driver"})
		Expect(err).To(HaveOccurred())
	})

	It("does not match the same pod twice", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{
					clientPod,
				},
			},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{
			"role=client",
			"role=client",
		})
		Expect(err).To(HaveOccurred())
	})

	It("returns successfully when all matching pods are found", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		client2Pod := newTestPod("client")
		client2Pod.Name = "client-2"
		client2Pod.Status.PodIP = "127.0.0.4"

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{
					driverPod,
					serverPod,
					clientPod,
					client2Pod,
				},
			},
		}

		podAddresses, err := WaitForReadyPods(ctx, mock, []string{
			"role=driver",
			"role=server",
			"role=client",
			"role=client",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(podAddresses).To(Equal([]string{
			fmt.Sprintf("%s:%d", driverPod.Status.PodIP, DefaultDriverPort),
			fmt.Sprintf("%s:%d", serverPod.Status.PodIP, DefaultDriverPort),
			fmt.Sprintf("%s:%d", clientPod.Status.PodIP, DefaultDriverPort),
			fmt.Sprintf("%s:%d", client2Pod.Status.PodIP, DefaultDriverPort),
		}))
	})

	It("returns with correct ports for matching pods", func() {
		ctx, cancel := context.WithTimeout(context.Background(), slowDuration)
		defer cancel()

		var customPort int32 = 9542
		client2Pod := newTestPod("client")
		client2Pod.Name = "client-2"
		client2PodContainer := kubehelpers.ContainerForName(config.RunContainerName, client2Pod.Spec.Containers)
		client2PodContainer.Ports[0].ContainerPort = customPort

		mock := &PodListerMock{
			PodList: &corev1.PodList{
				Items: []corev1.Pod{
					clientPod,
					client2Pod,
				},
			},
		}

		podAddresses, err := WaitForReadyPods(ctx, mock, []string{
			"role=client",
			"role=client",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(podAddresses).To(Equal([]string{
			fmt.Sprintf("%s:%d", clientPod.Status.PodIP, DefaultDriverPort),
			fmt.Sprintf("%s:%d", client2Pod.Status.PodIP, customPort),
		}))
	})

	It("returns error if timeout exceeded", func() {
		ctx, cancel := context.WithTimeout(context.Background(), fastDuration)
		defer cancel()

		mock := &PodListerMock{
			SleepDuration: slowDuration,
			PodList:       &corev1.PodList{},
		}

		_, err := WaitForReadyPods(ctx, mock, []string{"example"})
		Expect(err).To(HaveOccurred())
	})
})

type PodListerMock struct {
	PodList       *corev1.PodList
	SleepDuration time.Duration
	Error         error
	invocation    int
}

func (plm *PodListerMock) List(opts metav1.ListOptions) (*corev1.PodList, error) {
	time.Sleep(plm.SleepDuration)

	if plm.Error != nil {
		return nil, plm.Error
	}

	return plm.PodList, nil
}
