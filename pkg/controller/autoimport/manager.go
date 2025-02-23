// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package autoimport

import (
	"strings"

	workv1 "open-cluster-management.io/api/work/v1"

	"github.com/stolostron/managedcluster-import-controller/pkg/constants"
	"github.com/stolostron/managedcluster-import-controller/pkg/helpers"
	"github.com/stolostron/managedcluster-import-controller/pkg/source"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	runtimesource "sigs.k8s.io/controller-runtime/pkg/source"
)

const controllerName = "autoimport-controller"

// Add creates a new autoimport controller and adds it to the Manager.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, clientHolder *helpers.ClientHolder,
	importSecretInformer, autoImportSecretInformer cache.SharedIndexInformer) (string, error) {
	return controllerName, add(importSecretInformer, autoImportSecretInformer, mgr, newReconciler(clientHolder))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(clientHolder *helpers.ClientHolder) reconcile.Reconciler {
	return &ReconcileAutoImport{
		client:     clientHolder.RuntimeClient,
		kubeClient: clientHolder.KubeClient,
		recorder:   helpers.NewEventRecorder(clientHolder.KubeClient, controllerName),
	}
}

// adds a new Controller to mgr with r as the reconcile.Reconciler
func add(importSecretInformer, autoImportSecretInformer cache.SharedIndexInformer, mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New(controllerName, mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: helpers.GetMaxConcurrentReconciles(),
	})
	if err != nil {
		return err
	}

	// watch the import secrets
	if err := c.Watch(
		source.NewImportSecretSource(importSecretInformer),
		&source.ManagedClusterSecretEventHandler{},
		predicate.Predicate(predicate.Funcs{
			GenericFunc: func(e event.GenericEvent) bool { return false },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			CreateFunc:  func(e event.CreateEvent) bool { return true },
			UpdateFunc: func(e event.UpdateEvent) bool {
				new, okNew := e.ObjectNew.(*corev1.Secret)
				old, okOld := e.ObjectOld.(*corev1.Secret)
				if okNew && okOld {
					return !equality.Semantic.DeepEqual(old.Data, new.Data)
				}
				return false
			},
		}),
	); err != nil {
		return err
	}

	// watch the auto-import secrets
	if err := c.Watch(
		source.NewAutoImportSecretSource(autoImportSecretInformer),
		&source.ManagedClusterSecretEventHandler{},
		predicate.Predicate(predicate.Funcs{
			GenericFunc: func(e event.GenericEvent) bool { return false },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			CreateFunc:  func(e event.CreateEvent) bool { return true },
			UpdateFunc: func(e event.UpdateEvent) bool {
				new, okNew := e.ObjectNew.(*corev1.Secret)
				old, okOld := e.ObjectOld.(*corev1.Secret)
				if okNew && okOld {
					return !equality.Semantic.DeepEqual(old.Data, new.Data)
				}
				return false
			},
		}),
	); err != nil {
		return err
	}

	// watch the klusterlet manifest works
	if err := c.Watch(
		&runtimesource.Kind{Type: &workv1.ManifestWork{}},
		handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: o.GetNamespace(),
						Name:      o.GetNamespace(),
					},
				},
			}
		}),
		predicate.Predicate(predicate.Funcs{
			GenericFunc: func(e event.GenericEvent) bool { return false },
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			CreateFunc: func(e event.CreateEvent) bool {
				workName := e.Object.GetName()
				// only watch klusterlet manifest works
				if !strings.HasSuffix(workName, constants.KlusterletCRDsSuffix) &&
					!strings.HasSuffix(workName, constants.KlusterletSuffix) {
					return false
				}

				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				workName := e.ObjectNew.GetName()
				// only watch klusterlet manifest works
				if !strings.HasSuffix(workName, constants.KlusterletCRDsSuffix) &&
					!strings.HasSuffix(workName, constants.KlusterletSuffix) {
					return false
				}

				new, okNew := e.ObjectNew.(*workv1.ManifestWork)
				old, okOld := e.ObjectOld.(*workv1.ManifestWork)
				if okNew && okOld {
					return !helpers.ManifestsEqual(new.Spec.Workload.Manifests, old.Spec.Workload.Manifests)
				}

				return false
			},
		}),
	); err != nil {
		return err
	}

	return nil
}
