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

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	failoverv1alpha1 "github.com/mycreepy/failover-service-operator/api/v1alpha1"
)

const serviceDefaultSuffix = "-failover"
const statefulsetPodNameLabel = "statefulset.kubernetes.io/pod-name"

// FailoverServiceReconciler reconciles a FailoverService object
type FailoverServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=failover.mycreepy.github.io,resources=failoverservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=failover.mycreepy.github.io,resources=failoverservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=failover.mycreepy.github.io,resources=failoverservices/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *FailoverServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(5).Info("starting reconciliation")

	fos := failoverv1alpha1.FailoverService{}

	err := r.Get(ctx, req.NamespacedName, &fos)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(fos.Status.ActiveTarget) > 0 {
		err = r.reconcileActiveTarget(ctx, fos)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	err = r.createNewService(ctx, fos)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *FailoverServiceReconciler) createNewService(ctx context.Context, fos failoverv1alpha1.FailoverService) error {
	hls, err := r.getHeadlessService(ctx, fos.Namespace, fos.Spec.HeadlessServiceName)
	if err != nil {
		return err
	}

	eps, err := r.getEndpointSliceFromHeadlessService(ctx, fos.Namespace, fos.Spec.HeadlessServiceName)
	if err != nil {
		return err
	}

	target, err := r.findNextTarget(eps)
	if err != nil {
		return err
	}

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateServiceName(fos),
			Namespace: fos.Namespace,
			Labels:    fos.Labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         failoverv1alpha1.GroupVersion.Identifier(),
					Kind:               "FailoverService",
					Name:               fos.Name,
					UID:                fos.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: hls.Spec.Ports,
			Selector: map[string]string{
				statefulsetPodNameLabel: target,
			},
		},
	}

	err = r.Create(ctx, &svc)
	if err != nil {
		return err
	}

	fos.Status.ActiveTarget = target
	fos.Status.LastTransition = metav1.Now()

	return r.Status().Update(ctx, &fos)
}

func (r *FailoverServiceReconciler) getHeadlessService(ctx context.Context, ns, hls string) (corev1.Service, error) {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hls,
			Namespace: ns,
		},
	}

	err := r.Get(ctx, client.ObjectKeyFromObject(&svc), &svc)
	if err != nil {
		return corev1.Service{}, err
	}

	return svc, nil
}

func (r *FailoverServiceReconciler) reconcileActiveTarget(ctx context.Context, fos failoverv1alpha1.FailoverService) error {
	eps, err := r.getEndpointSliceFromHeadlessService(ctx, fos.Namespace, fos.Spec.HeadlessServiceName)
	if err != nil {
		return err
	}

	if r.isActiveTargetReady(fos.Status.ActiveTarget, eps) {
		return nil
	}

	nextTarget, err := r.findNextTarget(eps)
	if err != nil {
		return err
	}

	return r.setTarget(ctx, fos, nextTarget)
}

func (r *FailoverServiceReconciler) isActiveTargetReady(activeTarget string, eps discoveryv1.EndpointSlice) bool {
	var activeEndpoint discoveryv1.Endpoint

	for _, ep := range eps.Endpoints {
		if activeTarget != ep.TargetRef.Name {
			continue
		}

		activeEndpoint = ep
	}

	return isEndpointReady(activeEndpoint)
}

func (r *FailoverServiceReconciler) findNextTarget(eps discoveryv1.EndpointSlice) (string, error) {
	var nextTarget string

	for _, ep := range eps.Endpoints {
		if !isEndpointReady(ep) {
			continue
		}

		nextTarget = ep.TargetRef.Name
	}

	if len(nextTarget) == 0 {
		return "", fmt.Errorf("unable to find target: no endpoint is ready")
	}

	return nextTarget, nil
}

func (r *FailoverServiceReconciler) getEndpointSliceFromHeadlessService(ctx context.Context, ns string, hls string) (discoveryv1.EndpointSlice, error) {
	list := discoveryv1.EndpointSliceList{}

	req, err := labels.NewRequirement("kubernetes.io/service-name", selection.Equals, []string{hls})
	if err != nil {
		return discoveryv1.EndpointSlice{}, err
	}

	selector := labels.NewSelector()
	selector = selector.Add(*req)

	err = r.List(ctx, &list, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     ns,
	})
	if err != nil {
		return discoveryv1.EndpointSlice{}, err
	}

	if len(list.Items) != 1 {
		return discoveryv1.EndpointSlice{}, fmt.Errorf("unexpected count of endpointslices: %d", len(list.Items))
	}

	return list.Items[0], nil
}

func (r *FailoverServiceReconciler) setTarget(ctx context.Context, fos failoverv1alpha1.FailoverService, target string) error {
	svc, err := r.getServiceFromFailoverService(ctx, fos)
	if err != nil {
		return err
	}

	svc.Spec.Selector = map[string]string{
		statefulsetPodNameLabel: target,
	}

	err = r.Update(ctx, &svc)
	if err != nil {
		return err
	}

	fos.Status.ActiveTarget = target
	fos.Status.LastTransition = metav1.Now()

	return r.Status().Update(ctx, &fos)
}

func (r *FailoverServiceReconciler) getServiceFromFailoverService(ctx context.Context, fos failoverv1alpha1.FailoverService) (corev1.Service, error) {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateServiceName(fos),
			Namespace: fos.Namespace,
		},
	}

	err := r.Get(ctx, client.ObjectKeyFromObject(&svc), &svc)
	if err != nil {
		return corev1.Service{}, err
	}

	return svc, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FailoverServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&failoverv1alpha1.FailoverService{}).
		Complete(r)
}

func generateServiceName(fos failoverv1alpha1.FailoverService) string {
	serviceName := fos.Name + serviceDefaultSuffix
	if len(fos.Spec.ServiceName) > 0 {
		serviceName = fos.Spec.ServiceName
	}

	return serviceName
}

func isEndpointReady(ep discoveryv1.Endpoint) bool {
	if ep.Conditions.Ready == nil || ep.Conditions.Serving == nil || ep.Conditions.Terminating == nil {
		return false
	}

	return *ep.Conditions.Ready && *ep.Conditions.Serving && !*ep.Conditions.Terminating
}
