package controller

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
)

func tamossPrimaryPredicate() predicate.Predicate {
	return predicate.Or(
		primaryResourcePredicate(tamossFinalizer, []string{
			AnnotationSchemaRetry,
			AnnotationAPITokenRotate,
		}),
		predicate.Funcs{
			UpdateFunc: func(evt event.UpdateEvent) bool {
				oldTamoss, oldOK := evt.ObjectOld.(*tamossv1alpha1.Tamoss)
				newTamoss, newOK := evt.ObjectNew.(*tamossv1alpha1.Tamoss)
				if !oldOK || !newOK {
					return false
				}
				return !reflect.DeepEqual(oldTamoss.Status.Lifecycle, newTamoss.Status.Lifecycle)
			},
		},
	)
}

func storageBackendPrimaryPredicate() predicate.Predicate {
	return primaryResourcePredicate(storageBackendFinalizer, nil)
}

// credentialSecretPredicate passes Secret create and delete events but only
// forwards updates whose data actually changed, so metadata-only churn does
// not fan out into StorageBackend reconciles.
func credentialSecretPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(evt event.UpdateEvent) bool {
			oldSecret, oldOK := evt.ObjectOld.(*corev1.Secret)
			newSecret, newOK := evt.ObjectNew.(*corev1.Secret)
			if !oldOK || !newOK {
				return true
			}
			if oldSecret.ResourceVersion == newSecret.ResourceVersion {
				return false
			}
			return !reflect.DeepEqual(oldSecret.Data, newSecret.Data)
		},
	}
}

func primaryResourcePredicate(finalizer string, actionAnnotations []string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(event.GenericEvent) bool {
			return true
		},
		UpdateFunc: func(evt event.UpdateEvent) bool {
			oldObj := evt.ObjectOld
			newObj := evt.ObjectNew
			if oldObj == nil || newObj == nil {
				return true
			}
			if !oldObj.GetDeletionTimestamp().Equal(newObj.GetDeletionTimestamp()) {
				return true
			}
			if oldObj.GetGeneration() != newObj.GetGeneration() {
				return true
			}
			if !reflect.DeepEqual(oldObj.GetFinalizers(), newObj.GetFinalizers()) {
				return true
			}
			for _, key := range actionAnnotations {
				if oldObj.GetAnnotations()[key] != newObj.GetAnnotations()[key] {
					return true
				}
			}
			return false
		},
	}
}
