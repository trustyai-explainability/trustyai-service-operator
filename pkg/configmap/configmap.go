package configmap

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Get retrieves a ConfigMap by name in the given namespace.
// Returns an error if the ConfigMap is not found or cannot be read.
func Get(ctx context.Context, c client.Client, name, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("configmap %s not found in namespace %s", name, namespace)
		}
		return nil, fmt.Errorf("error reading configmap %s in namespace %s: %w", name, namespace, err)
	}
	return configMap, nil
}
