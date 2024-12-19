package guardrails

import (
	"context"
	"reflect"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const serviceAccountTemplatePath = "templates/service_account.yaml"

type ServiceAccountConfig struct {
	Name      string
	Namespace string
}

func createServiceAccountName(orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) string {
	return orchestrator.Name + "-service-account"
}

func (r *GuardrailsReconciler) ensureServiceAccount(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	existingServiceAccount := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, existingServiceAccount)

	if err != nil {
		if errors.IsNotFound(err) {
			return r.createServiceAccount(ctx, orchestrator)
		}
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) createServiceAccount(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	serviceAccountName := createServiceAccountName(orchestrator)

	serviceAccountConfig := ServiceAccountConfig{
		Name:      serviceAccountName,
		Namespace: orchestrator.Namespace,
	}

	var serviceAccount *corev1.ServiceAccount

	serviceAccount, err := templateParser.ParseResource[corev1.ServiceAccount](serviceAccountTemplatePath, serviceAccountConfig, reflect.TypeOf(&corev1.ServiceAccount{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to parse service account template")
		return err
	}

	controllerutil.SetControllerReference(orchestrator, serviceAccount, r.Scheme)

	err = r.Create(ctx, serviceAccount)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create service account")
		return err
	}

	err = r.createRoleBinding(ctx, orchestrator, serviceAccountName)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to create role binding")
		return err
	}
	return nil
}

func (r *GuardrailsReconciler) createRoleBinding(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, serviceAccountName string) error {

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name + "-" + orchestrator.Namespace + "-role-binding",
			Namespace: orchestrator.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "ServiceAccount",
				Name: serviceAccountName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: "guardrails-orchestrator-role",
		},
	}
	if err := controllerutil.SetControllerReference(orchestrator, roleBinding, r.Scheme); err != nil {
		return err
	}
	existingRoleBinding := &rbacv1.RoleBinding{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: roleBinding.Name}, existingRoleBinding)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating a new RoleBinding", "RoleBinding.Name", roleBinding.Name)
		err = r.Create(context.TODO(), roleBinding)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new RoleBinding")
			return err
		}
	}
	return nil
}
