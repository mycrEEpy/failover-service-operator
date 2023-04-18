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
	"time"

	failoverv1alpha1 "github.com/mycreepy/failover-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ServiceDefaultSuffix     = "-failover"
	StatefulsetPodNameLabel  = "statefulset.kubernetes.io/pod-name"
	ConditionTypeReady       = "Ready"
	ConditionReasonSucceeded = "ReconciliationSucceeded"
	ConditionReasonFailed    = "ReconciliationFailed"
)

// FailoverServiceReconciler reconciles a FailoverService object
type FailoverServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Interval time.Duration
}

//+kubebuilder:rbac:groups=mycreepy.github.io,resources=failoverservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mycreepy.github.io,resources=failoverservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mycreepy.github.io,resources=failoverservices/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=services,verbs=get;create;patch
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *FailoverServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&failoverv1alpha1.FailoverService{}).
		Complete(r)
}

// Start will reconcile all FailoverServices and will start the busy loop.
func (r *FailoverServiceReconciler) Start(ctx context.Context) error {
	err := r.reconcileAll(ctx)
	if err != nil {
		return err
	}

	r.runBusyLoop(ctx, r.Interval)

	return nil
}

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
		if kubeErrors.IsNotFound(err) {
			logger.Info("FailoverService has been deleted")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	original := fos.DeepCopy()

	// Initial setup for the service
	if len(fos.Status.ActiveTarget) == 0 {
		svc, err := r.createNewService(ctx, fos)
		if err != nil {
			apiMeta.SetStatusCondition(&fos.Status.Conditions, metav1.Condition{
				Type:    ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  ConditionReasonFailed,
				Message: fmt.Sprintf("failed to create new service: %s", err),
			})

			err = r.Status().Patch(ctx, &fos, client.MergeFrom(original))
			if err != nil {
				logger.Error(err, "status update failed")
			}

			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}

		err = r.updateServiceSelector(ctx, fos)
		if err != nil {
			apiMeta.SetStatusCondition(&fos.Status.Conditions, metav1.Condition{
				Type:    ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  ConditionReasonFailed,
				Message: fmt.Sprintf("failed to update service selector: %s", err),
			})

			err = r.Status().Patch(ctx, &fos, client.MergeFrom(original))
			if err != nil {
				logger.Error(err, "status update failed")
			}

			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}

		logger.Info("service successfully initialized", "serviceName", svc.Name)
	}

	nextTarget, err := r.reconcileActiveTarget(ctx, fos)
	if err != nil {
		apiMeta.SetStatusCondition(&fos.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ConditionReasonFailed,
			Message: fmt.Sprintf("failed to reconcile active target: %s", err),
		})

		err = r.Status().Patch(ctx, &fos, client.MergeFrom(original))
		if err != nil {
			logger.Error(err, "status update failed")
		}

		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	if nextTarget != fos.Status.ActiveTarget {
		logger.Info("new target", "target", nextTarget)
	}

	if !apiMeta.IsStatusConditionTrue(fos.Status.Conditions, ConditionTypeReady) {
		apiMeta.SetStatusCondition(&fos.Status.Conditions, metav1.Condition{
			Type:   ConditionTypeReady,
			Status: metav1.ConditionTrue,
			Reason: ConditionReasonSucceeded,
		})

		err = r.Status().Patch(ctx, &fos, client.MergeFrom(original))
		if err != nil {
			logger.Error(err, "status patch failed")
			return ctrl.Result{RequeueAfter: time.Second * 10}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileAll will call Reconcile on all FailoverServices.
func (r *FailoverServiceReconciler) reconcileAll(ctx context.Context) error {
	logger := log.FromContext(ctx)

	fosList := failoverv1alpha1.FailoverServiceList{}

	err := r.List(ctx, &fosList)
	if err != nil {
		return err
	}

	for _, fos := range fosList.Items {
		// TODO: ctx does not have a proper logger
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: fos.Namespace,
			Name:      fos.Name,
		},
		})
		if err != nil {
			logger.Error(err, "initial reconcile failed", "namespace", fos.Namespace, "name", fos.Name)
		}
	}

	return nil
}

// runBusyLoop will check all FailoverServices each interval.
func (r *FailoverServiceReconciler) runBusyLoop(ctx context.Context, interval time.Duration) {
	logger := log.FromContext(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			list := failoverv1alpha1.FailoverServiceList{}

			err := r.List(ctx, &list)
			if err != nil {
				logger.Error(err, "failed to list FailoverServices")
				continue
			}

			for _, fos := range list.Items {
				// Only check ready objects
				if !apiMeta.IsStatusConditionTrue(fos.Status.Conditions, ConditionTypeReady) {
					continue
				}

				// TODO: ctx does not have a proper logger
				_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
					Namespace: fos.Namespace,
					Name:      fos.Name,
				}})
				if err != nil {
					logger.Error(err, "reconcile failed", "namespace", fos.Namespace, "name", fos.Name)
				}
			}
		}
	}
}

func (r *FailoverServiceReconciler) createNewService(ctx context.Context, fos failoverv1alpha1.FailoverService) (*corev1.Service, error) {
	logger := log.FromContext(ctx)

	hls, err := r.getHeadlessService(ctx, fos.Namespace, fos.Spec.Service.Name)
	if err != nil {
		return nil, err
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
		},
	}

	err = r.Create(ctx, &svc)
	if err != nil {
		if kubeErrors.IsAlreadyExists(err) {
			return &svc, nil
		}

		return nil, err
	}

	logger.Info("new service created", "serviceName", svc.Name)

	return &svc, nil
}

func (r *FailoverServiceReconciler) updateServiceSelector(ctx context.Context, fos failoverv1alpha1.FailoverService) error {
	eps, err := r.getEndpointSliceFromService(ctx, fos.Namespace, fos.Spec.Service.Name)
	if err != nil {
		return err
	}

	target, err := r.findNextTarget(eps)
	if err != nil {
		return err
	}

	return r.setTarget(ctx, fos, target)
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

func (r *FailoverServiceReconciler) reconcileActiveTarget(ctx context.Context, fos failoverv1alpha1.FailoverService) (string, error) {
	eps, err := r.getEndpointSliceFromService(ctx, fos.Namespace, fos.Spec.Service.Name)
	if err != nil {
		return "", err
	}

	var availableTargets int
	for _, ep := range eps.Endpoints {
		if isEndpointReady(ep) {
			availableTargets++
		}
	}

	if availableTargets != fos.Status.AvailableTargets {
		err = r.setAvailableTargets(ctx, fos, availableTargets)
		if err != nil {
			return "", err
		}
	}

	if r.isActiveTargetReady(fos.Status.ActiveTarget, eps) {
		return fos.Status.ActiveTarget, nil
	}

	nextTarget, err := r.findNextTarget(eps)
	if err != nil {
		return "", err
	}

	err = r.setTarget(ctx, fos, nextTarget)
	if err != nil {
		return "", err
	}

	return nextTarget, nil
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

func (r *FailoverServiceReconciler) getEndpointSliceFromService(ctx context.Context, ns string, svc string) (discoveryv1.EndpointSlice, error) {
	list := discoveryv1.EndpointSliceList{}

	req, err := labels.NewRequirement("kubernetes.io/service-name", selection.Equals, []string{svc})
	if err != nil {
		return discoveryv1.EndpointSlice{}, err
	}

	selector := labels.NewSelector()
	selector = selector.Add(*req)

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = wait.PollImmediateUntilWithContext(timeoutCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
		listErr := r.List(ctx, &list, &client.ListOptions{
			LabelSelector: selector,
			Namespace:     ns,
		})
		if listErr != nil {
			return false, nil
		}

		return true, nil
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

	originalSvc := svc.DeepCopy()

	svc.Spec.Selector = map[string]string{
		StatefulsetPodNameLabel: target,
	}

	err = r.Patch(ctx, &svc, client.MergeFrom(originalSvc))
	if err != nil {
		return err
	}

	originalFos := fos.DeepCopy()

	fos.Status.ActiveTarget = target
	fos.Status.LastTransition = metav1.Now()

	return r.Status().Patch(ctx, &fos, client.MergeFrom(originalFos))
}

func (r *FailoverServiceReconciler) setAvailableTargets(ctx context.Context, fos failoverv1alpha1.FailoverService, available int) error {
	originalFos := fos.DeepCopy()

	fos.Status.AvailableTargets = available

	return r.Status().Patch(ctx, &fos, client.MergeFrom(originalFos))
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

func generateServiceName(fos failoverv1alpha1.FailoverService) string {
	serviceName := fos.Name + ServiceDefaultSuffix
	if len(fos.Spec.Service.Suffix) > 0 {
		serviceName = fos.Name + fos.Spec.Service.Suffix
	}

	return serviceName
}

func isEndpointReady(ep discoveryv1.Endpoint) bool {
	if ep.Conditions.Ready == nil || ep.Conditions.Serving == nil || ep.Conditions.Terminating == nil {
		return false
	}

	return *ep.Conditions.Ready && *ep.Conditions.Serving && !*ep.Conditions.Terminating
}
