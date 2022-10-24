/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	camundaiov1alpha1 "github.com/sijoma/zeebe-play-operator/api/v1alpha1"
)

var _ = Describe("ZeebePlay controller", func() {
	Context("ZeebePlay controller test", func() {

		const ZeebePlayName = "test-zeebeplay"

		ctx := context.Background()

		typeNamespaceName := types.NamespacedName{Name: ZeebePlayName, Namespace: ZeebePlayName}

		BeforeEach(func() {
			By("Setting the Image ENV VAR which stores the Operand image")
			err := os.Setenv("ZEEBEPLAY_IMAGE", "example.com/image:test")
			Expect(err).To(Not(HaveOccurred()))
		})

		AfterEach(func() {
			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("ZEEBEPLAY_IMAGE")
		})

		It("should successfully reconcile a custom resource for ZeebePlay", func() {
			By("Creating the custom resource for the Kind ZeebePlay")
			zeebeplay := &camundaiov1alpha1.ZeebePlay{}
			err := k8sClient.Get(ctx, typeNamespaceName, zeebeplay)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				zeebeplay := &camundaiov1alpha1.ZeebePlay{
					ObjectMeta: metav1.ObjectMeta{
						Name: ZeebePlayName,
					},
					Spec: camundaiov1alpha1.ZeebePlaySpec{
						Size: 1,
					},
				}

				err = k8sClient.Create(ctx, zeebeplay)
				Expect(err).To(Not(HaveOccurred()))
			}

			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &camundaiov1alpha1.ZeebePlay{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			zeebeplayReconciler := &ZeebePlayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = zeebeplayReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			_, err = zeebeplayReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			_, err = zeebeplayReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Ingress was successfully created in the reconciliation")
			Eventually(func() error {
				found := &v1.Ingress{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking if Service was successfully created in the reconciliation")
			Eventually(func() error {
				found := &corev1.Service{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the ZeebePlay instance")
			Eventually(func() error {
				if zeebeplay.Status.Conditions != nil && len(zeebeplay.Status.Conditions) != 0 {
					latestStatusCondition := zeebeplay.Status.Conditions[len(zeebeplay.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{Type: typeAvailableZeebePlay,
						Status: metav1.ConditionTrue, Reason: "Reconciling",
						Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", zeebeplay.Name, zeebeplay.Spec.Size)}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the zeebeplay instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
