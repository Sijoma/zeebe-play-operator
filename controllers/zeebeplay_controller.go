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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	camundaiov1alpha1 "github.com/sijoma/zeebe-play-operator/api/v1alpha1"
)

const zeebeplayFinalizer = "play.camunda.io/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableZeebePlay represents the status of the Deployment reconciliation
	typeAvailableZeebePlay = "Available"
	// typeDegradedZeebePlay represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedZeebePlay = "Degraded"
)

// ZeebePlayReconciler reconciles a ZeebePlay object
type ZeebePlayReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

//+kubebuilder:rbac:groups=play.camunda.io,resources=zeebeplays,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=play.camunda.io,resources=zeebeplays/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=play.camunda.io,resources=zeebeplays/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=namespaces;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *ZeebePlayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the ZeebePlay instance
	// The purpose is check if the Custom Resource for the Kind ZeebePlay
	// is applied on the cluster if not we return nil to stop the reconciliation
	zeebeplay := &camundaiov1alpha1.ZeebePlay{}
	err := r.Get(ctx, req.NamespacedName, zeebeplay)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("zeebeplay resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get zeebeplay")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status are available
	if zeebeplay.Status.Conditions == nil || len(zeebeplay.Status.Conditions) == 0 {
		meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeAvailableZeebePlay, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, zeebeplay); err != nil {
			log.Error(err, "Failed to update ZeebePlay status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the zeebeplay Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, zeebeplay); err != nil {
			log.Error(err, "Failed to re-fetch zeebeplay")
			return ctrl.Result{}, err
		}
	}

	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(zeebeplay, zeebeplayFinalizer) {
		log.Info("Adding Finalizer for ZeebePlay")
		if ok := controllerutil.AddFinalizer(zeebeplay, zeebeplayFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, zeebeplay); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the ZeebePlay instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isZeebePlayMarkedToBeDeleted := zeebeplay.GetDeletionTimestamp() != nil
	if isZeebePlayMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(zeebeplay, zeebeplayFinalizer) {
			log.Info("Performing Finalizer Operations for ZeebePlay before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeDegradedZeebePlay,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", zeebeplay.Name)})

			if err := r.Status().Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to update ZeebePlay status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom custom resource.
			r.doFinalizerOperationsForZeebePlay(zeebeplay)

			// TODO(user): If you add operations to the doFinalizerOperationsForZeebePlay method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the zeebeplay Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, zeebeplay); err != nil {
				log.Error(err, "Failed to re-fetch zeebeplay")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeDegradedZeebePlay,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", zeebeplay.Name)})

			if err := r.Status().Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to update ZeebePlay status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for ZeebePlay after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(zeebeplay, zeebeplayFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for ZeebePlay")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to remove finalizer for ZeebePlay")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if time.Now().After(zeebeplay.Spec.DeathDate.Time) {
		err = r.Delete(ctx, zeebeplay)
		log.Error(err, "unable to delete expired zeebe-play instance")
		return ctrl.Result{}, err
	}

	namespace := new(corev1.Namespace)
	namespace.SetName(zeebeplay.Name)
	_, err = ctrl.CreateOrUpdate(ctx, r.Client, namespace, func() error {
		return ctrl.SetControllerReference(zeebeplay, namespace, r.Scheme)
	})
	if err != nil {
		log.Error(err, "unable to create namespace")
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: zeebeplay.Name, Namespace: namespace.Name}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForZeebePlay(zeebeplay)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for ZeebePlay")

			// The following implementation will update the status
			meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeAvailableZeebePlay,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", zeebeplay.Name, err)})

			if err := r.Status().Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to update ZeebePlay status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment",
			"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment",
				"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if the service already exists, if not create a new one
	foundService := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: zeebeplay.Name, Namespace: namespace.Name}, foundService)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		sev, err := r.serviceForZeebePlay(zeebeplay)
		if err != nil {
			log.Error(err, "Failed to define new Service resource for ZeebePlay")

			// The following implementation will update the status
			meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeAvailableZeebePlay,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Service for the custom resource (%s): (%s)", zeebeplay.Name, err)})

			if err := r.Status().Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to update ZeebePlay status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Service",
			"Service.Namespace", sev.Namespace, "Service.Name", sev.Name)
		if err = r.Create(ctx, sev); err != nil {
			log.Error(err, "Failed to create new Service",
				"Service.Namespace", sev.Namespace, "Service.Name", sev.Name)
			return ctrl.Result{}, err
		}

		// Service created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if the service already exists, if not create a new one
	foundIngress := &networkingv1.Ingress{}
	err = r.Get(ctx, types.NamespacedName{Name: zeebeplay.Name, Namespace: namespace.Name}, foundIngress)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		ing, err := r.ingressForZeebePlay(zeebeplay)
		if err != nil {
			log.Error(err, "Failed to define new Ingress resource for ZeebePlay")

			// The following implementation will update the status
			meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeAvailableZeebePlay,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Ingress for the custom resource (%s): (%s)", zeebeplay.Name, err)})

			if err := r.Status().Update(ctx, zeebeplay); err != nil {
				log.Error(err, "Failed to update ZeebePlay status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Ingress",
			"Ingress.Namespace", ing.Namespace, "Ingress.Name", ing.Name)
		if err = r.Create(ctx, ing); err != nil {
			log.Error(err, "Failed to create new Ingress",
				"Ingress.Namespace", ing.Namespace, "Ingress.Name", ing.Name)
			return ctrl.Result{}, err
		}

		// Service created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Ingress")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&zeebeplay.Status.Conditions, metav1.Condition{Type: typeAvailableZeebePlay,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) created successfully", zeebeplay.Name)})

	// We are sure that these are already set.
	httpHost := foundIngress.Spec.Rules[0].Host
	grpcHost := foundIngress.Spec.Rules[1].Host

	zeebeplay.Status.HttpEndpoint = httpHost
	zeebeplay.Status.GrpcEndpoint = grpcHost

	if err := r.Status().Update(ctx, zeebeplay); err != nil {
		log.Error(err, "Failed to update ZeebePlay status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeZeebePlay will perform the required operations before delete the CR.
func (r *ZeebePlayReconciler) doFinalizerOperationsForZeebePlay(cr *camundaiov1alpha1.ZeebePlay) {
	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// deploymentForZeebePlay returns a ZeebePlay Deployment object
func (r *ZeebePlayReconciler) deploymentForZeebePlay(
	zeebeplay *camundaiov1alpha1.ZeebePlay) (*appsv1.Deployment, error) {
	ls := labelsForZeebePlay(zeebeplay.Name)
	replicas := int32(1)

	// Get the Operand image
	image, err := imageForZeebePlay()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      zeebeplay.Name,
			Namespace: zeebeplay.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "zeebeplay",
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8080,
								Name:          "http",
							},
							{
								ContainerPort: 26500,
								Name:          "grpc",
							},
						},
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(zeebeplay, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func (r *ZeebePlayReconciler) serviceForZeebePlay(zeebeplay *camundaiov1alpha1.ZeebePlay) (*corev1.Service, error) {
	ls := labelsForZeebePlay(zeebeplay.Name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      zeebeplay.Name,
			Namespace: zeebeplay.Name,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					Port: 8080,
					Name: "http",
				},
				{
					Port: 26500,
					Name: "grpc",
				},
			},
		},
	}

	// Set the ownerRef for the Service
	if err := ctrl.SetControllerReference(zeebeplay, service, r.Scheme); err != nil {
		return nil, err
	}
	return service, nil
}

func (r *ZeebePlayReconciler) ingressForZeebePlay(zeebeplay *camundaiov1alpha1.ZeebePlay) (*networkingv1.Ingress, error) {
	ingressClassName := "nginx"
	pathType := networkingv1.PathTypeImplementationSpecific
	domain := ".play.ultrawombat.com"

	ingressServiceBackendHTTP := networkingv1.IngressServiceBackend{
		Name: zeebeplay.Name,
		Port: networkingv1.ServiceBackendPort{
			Name: "http",
		},
	}

	ingressServiceBackendGRPC := networkingv1.IngressServiceBackend{
		Name: zeebeplay.Name,
		Port: networkingv1.ServiceBackendPort{
			Name: "grpc",
		},
	}
	ingressRuleValueHTTP := networkingv1.HTTPIngressRuleValue{
		Paths: []networkingv1.HTTPIngressPath{
			{
				PathType: &pathType,
				Backend: networkingv1.IngressBackend{
					Service: &ingressServiceBackendHTTP,
				},
			},
		},
	}

	ingressRuleValueGRPC := networkingv1.HTTPIngressRuleValue{
		Paths: []networkingv1.HTTPIngressPath{
			{
				PathType: &pathType,
				Backend: networkingv1.IngressBackend{
					Service: &ingressServiceBackendGRPC,
				},
			},
		},
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      zeebeplay.Name,
			Namespace: zeebeplay.Name,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: zeebeplay.Name + domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &ingressRuleValueHTTP,
					},
				},
				{
					Host: zeebeplay.Name + "-grpc" + domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &ingressRuleValueGRPC,
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts: []string{
						zeebeplay.Name + domain,
						zeebeplay.Name + "-grpc" + domain,
					},
				},
			},
		},
	}

	// Set the ownerRef for the Service
	if err := ctrl.SetControllerReference(zeebeplay, ingress, r.Scheme); err != nil {
		return nil, err
	}
	return ingress, nil
}

// labelsForZeebePlay returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForZeebePlay(name string) map[string]string {
	var imageTag string
	image, err := imageForZeebePlay()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{"app.kubernetes.io/name": "ZeebePlay",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "zeebe-play-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// imageForZeebePlay gets the Operand image which is managed by this controller
// from the ZEEBEPLAY_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForZeebePlay() (string, error) {
	var imageEnvVar = "ZEEBEPLAY_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("Unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
// Note that the Deployment will be also watched in order to ensure its
// desirable state on the cluster
func (r *ZeebePlayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&camundaiov1alpha1.ZeebePlay{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
