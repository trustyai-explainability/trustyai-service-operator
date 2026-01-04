package utils

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AddFinalizerIfNeeded will add the deletion finalizer to an object if it has not yet already been applied
func AddFinalizerIfNeeded(ctx context.Context, c client.Client, owner client.Object, finalizerName string) error {
	if !controllerutil.ContainsFinalizer(owner, finalizerName) {
		LogInfoVerb(ctx, "adding", "finalizer", owner.GetName(), owner.GetNamespace())
		if ok := controllerutil.AddFinalizer(owner, finalizerName); !ok {
			err := fmt.Errorf("failed to add finalizer")
			LogErrorVerb(ctx, err, "adding", "finalizer", owner.GetName(), owner.GetNamespace())
			return err
		}
		if err := c.Update(ctx, owner); err != nil {
			LogErrorVerb(ctx, err, "updating", "custom resource", owner.GetName(), owner.GetNamespace())
			return err
		}
	}
	return nil
}

// HandleDeletionIfNeeded checks if the object is marked for deletion and contains the given finalizer.
// if so, it will run the objects cleanupFunction and then remove the finalizer
// returns a bool indicating if the reconcile function should exit upon completion. this will be true if there was an error or if deletion has occurred
func HandleDeletionIfNeeded(ctx context.Context, c client.Client, owner client.Object, finalizerName string, cleanupFunc func() error) (bool, error) {
	if owner.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(owner, finalizerName) {
		log.FromContext(ctx).Info("inside handle deletion", "deletion timestamp", owner.GetDeletionTimestamp())
		namespacedName := types.NamespacedName{Name: owner.GetName(), Namespace: owner.GetNamespace()}
		if err := c.Get(ctx, namespacedName, owner); err != nil {
			LogErrorVerb(ctx, err, "refreshing", "custom resource", owner.GetName(), owner.GetNamespace())
			return true, err
		}

		// Call the cleanup function
		if cleanupFunc != nil {
			if err := cleanupFunc(); err != nil {
				return true, err
			}
		}

		// Remove the finalizer
		if ok := controllerutil.RemoveFinalizer(owner, finalizerName); !ok {
			err := fmt.Errorf("failed to remove finalizer")
			LogErrorVerb(ctx, err, "removing", "finalizer", owner.GetName(), owner.GetNamespace())
			return true, err
		}
		// Update the object to persist the removal
		if err := c.Update(ctx, owner); err != nil {
			LogErrorVerb(ctx, err, "updating", "custom resource (removing finalizer)", owner.GetName(), owner.GetNamespace())
			return true, err
		}
		return true, nil

	}
	return false, nil
}
