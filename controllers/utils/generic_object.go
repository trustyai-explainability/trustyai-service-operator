package utils

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type GenericConfig struct {
	Name         *string
	Namespace    *string
	SpecificData interface{}
}

// GetGenericConfig converts a specific resource template config into a generic one that can be used in the Generic functions below
/*
The GenericConfig is essentially a wrapper around the original Config object, with the Name and Namespace specifically
extracted. In a more object-oriented language, this might look like "RouteConfig extends GenericConfig", but that's not possible in Golang
*/
func GetGenericConfig(name *string, namespace *string, specificConfig interface{}) GenericConfig {
	return GenericConfig{
		Name:         name,
		Namespace:    namespace,
		SpecificData: specificConfig,
	}
}

// DefineGeneric defines a generic object of type T, but does not deploy it to the cluster.
/*
T must be a pointer to a k8s object, e.g., *corev1.ConfigMap

Returns:
- the newly created object
- any error
*/
func DefineGeneric[T client.Object](ctx context.Context, c client.Client, owner metav1.Object, resourceKind string, config GenericConfig, templatePath string, parser ResourceParserFunc[T]) (T, error) {
	// Parse the template of resource to define a new instance of the object
	obj, err := parser(templatePath, config.SpecificData, reflect.TypeOf(new(T)).Elem())

	if err != nil {
		LogErrorParsing(ctx, err, resourceKind, *config.Name, *config.Namespace)
		var zero T
		return zero, err
	}

	// Set owner of the new object
	err = controllerutil.SetControllerReference(owner, obj, c.Scheme())
	if err != nil {
		LogErrorControllerReference(ctx, err, resourceKind, *config.Name, *config.Namespace)
		var zero T
		return zero, err
	}
	return obj, nil
}

// ReconcileGenericManuallyDefined reconciles a generic object of type T, where the definition of the object is handled manually
/*
T must be a pointer to a k8s object, e.g., *corev1.ConfigMap

Returns:
- the created/found object
- a boolean flag indicating whether the returned object was created during the Reconcile call,
- any error
*/
func ReconcileGenericManuallyDefined[T client.Object](ctx context.Context, c client.Client, resourceKind string, owner metav1.Object, preDefinedObject T) (T, bool, error) {
	// Allocate a new pointer to the struct that T points to, and cast to T
	// e.g., replaces existingRoute := &routev1.Route{}
	var zero T
	var existingObj T
	tType := reflect.TypeOf((*T)(nil)).Elem()
	if tType.Kind() != reflect.Ptr {
		return zero, false, fmt.Errorf("T must be a pointer type")
	}
	existingObjValue := reflect.New(tType.Elem())
	existingObj = existingObjValue.Interface().(T)

	// attempt to retrieve a matching object from the cluster
	err := c.Get(ctx, types.NamespacedName{Name: preDefinedObject.GetName(), Namespace: preDefinedObject.GetNamespace()}, existingObj)
	if err != nil && errors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(owner, preDefinedObject, c.Scheme()); err != nil {
			LogErrorControllerReference(ctx, err, resourceKind, preDefinedObject.GetName(), preDefinedObject.GetNamespace())
			return zero, false, err
		}
		LogInfoCreating(ctx, resourceKind, preDefinedObject.GetName(), preDefinedObject.GetNamespace())

		// deploy the resource onto the cluster
		if err = c.Create(ctx, preDefinedObject); err != nil {
			LogErrorCreating(ctx, err, resourceKind, preDefinedObject.GetName(), preDefinedObject.GetNamespace())
			return zero, false, err
		}
		// we just created a new object, return the obj and true
		return preDefinedObject, true, nil
	} else if err != nil {
		LogErrorRetrieving(ctx, err, resourceKind, preDefinedObject.GetName(), preDefinedObject.GetNamespace())
		return zero, false, err
	}

	// object already existed, return the existingObj and false
	return existingObj, false, nil
}

// ReconcileGeneric reconciles a generic object of type T, where definition of the object is handled by a TemplateParser
/*
T must be a pointer to a k8s object, e.g., *corev1.ConfigMap

Returns:
- the created/found object
- a boolean flag indicating whether the returned object was created during the Reconcile call,
- any error
*/
func ReconcileGeneric[T client.Object](ctx context.Context, c client.Client, owner metav1.Object, resourceKind string, config GenericConfig, templatePath string, parserFunc ResourceParserFunc[T]) (T, bool, error) {
	// Allocate a new pointer to the struct that T points to, and cast to T
	// e.g., replaces existingRoute := &routev1.Route{}
	var zero T
	var existingObj T
	tType := reflect.TypeOf((*T)(nil)).Elem()
	if tType.Kind() != reflect.Ptr {
		return zero, false, fmt.Errorf("T must be a pointer type")
	}
	existingObjValue := reflect.New(tType.Elem())
	existingObj = existingObjValue.Interface().(T)

	// attempt to retrieve a matching object from the cluster
	err := c.Get(ctx, types.NamespacedName{Name: *config.Name, Namespace: *config.Namespace}, existingObj)
	if err != nil && errors.IsNotFound(err) {
		// Object that we want doesn't exist, so define a new instance of a resource according to the config
		obj, err := DefineGeneric[T](ctx, c, owner, resourceKind, config, templatePath, parserFunc)
		if err != nil {
			LogErrorDefining(ctx, err, resourceKind, *config.Name, *config.Namespace)
			return zero, false, err
		}
		LogInfoCreating(ctx, resourceKind, obj.GetName(), obj.GetNamespace())

		// deploy the resource onto the cluster
		err = c.Create(ctx, obj)
		if err != nil {
			LogErrorCreating(ctx, err, resourceKind, obj.GetName(), obj.GetNamespace())
			return zero, false, err
		}
		// we just created a new object, return the obj and true
		return obj, true, nil
	} else if err != nil {
		LogErrorRetrieving(ctx, err, resourceKind, *config.Name, *config.Namespace)
		return zero, false, err
	}

	// object already existed, return the existingObj and false
	return existingObj, false, nil
}
