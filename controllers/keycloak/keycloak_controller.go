package keycloak

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"

	keycloakApi "github.com/epam/edp-keycloak-operator/api/v1"
	"github.com/epam/edp-keycloak-operator/controllers/helper"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak"
)

type Helper interface {
	CreateKeycloakClientFromAuthData(ctx context.Context, authData *helper.KeycloakAuthData, caCertificate string) (keycloak.Client, error)
}

func NewReconcileKeycloak(client client.Client, scheme *runtime.Scheme, helper Helper) *ReconcileKeycloak {
	return &ReconcileKeycloak{
		client: client,
		scheme: scheme,
		helper: helper,
	}
}

// ReconcileKeycloak reconciles a Keycloak object.
type ReconcileKeycloak struct {
	client                  client.Client
	scheme                  *runtime.Scheme
	helper                  Helper
	successReconcileTimeout time.Duration
}

const connectionRetryPeriod = time.Second * 10

//+kubebuilder:rbac:groups=v1.edp.epam.com,namespace=placeholder,resources=keycloaks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=v1.edp.epam.com,namespace=placeholder,resources=keycloaks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=v1.edp.epam.com,namespace=placeholder,resources=keycloaks/finalizers,verbs=update
//+kubebuilder:rbac:groups="",namespace=placeholder,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",namespace=placeholder,resources=configmaps,verbs=get;list;watch

// Reconcile is a loop for reconciling Keycloak object.
func (r *ReconcileKeycloak) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling Keycloak")

	instance := &keycloakApi.Keycloak{}
	if err := r.client.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Instance not found")
			return reconcile.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to get keycloak instance: %w", err)
	}

	caCertificate, err := r.getCACertificate(ctx, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	if err := r.updateConnectionStatusToKeycloak(ctx, instance, caCertificate); err != nil {
		return reconcile.Result{}, err
	}

	if !instance.Status.Connected {
		log.Info("Keycloak is not connected, will retry")
		return reconcile.Result{RequeueAfter: connectionRetryPeriod}, nil
	}

	log.Info("Reconciling Keycloak has been finished")

	return reconcile.Result{}, nil
}

func (r *ReconcileKeycloak) SetupWithManager(mgr ctrl.Manager, successReconcileTimeout time.Duration) error {
	r.successReconcileTimeout = successReconcileTimeout

	pred := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}

	err := ctrl.NewControllerManagedBy(mgr).
		For(&keycloakApi.Keycloak{}, builder.WithPredicates(pred)).
		Complete(r)
	if err != nil {
		return fmt.Errorf("failed to setup Keycloak controller: %w", err)
	}

	return nil
}

func (r *ReconcileKeycloak) updateConnectionStatusToKeycloak(ctx context.Context, instance *keycloakApi.Keycloak, caCertificate string) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Start updating connection status to Keycloak")

	_, err := r.helper.CreateKeycloakClientFromAuthData(ctx, helper.MakeKeycloakAuthDataFromKeycloak(instance), caCertificate)
	if err != nil {
		log.Error(err, "Unable to connect to Keycloak")
	}

	connected := err == nil

	if instance.Status.Connected == connected {
		log.Info("Connection status hasn't been changed", "status", instance.Status.Connected)
		return nil
	}

	log.Info("Connection status has been changed", "from", instance.Status.Connected, "to", connected)

	instance.Status.Connected = connected

	err = r.client.Status().Update(ctx, instance)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.Info("Status has been updated", "status", instance.Status)

	return nil
}

func (r *ReconcileKeycloak) getCACertificate(ctx context.Context, instance *keycloakApi.Keycloak) (string, error) {
	if instance.Spec.Certificate != nil {
		if instance.Spec.Certificate.SecretName != "" {
			secret := &corev1.Secret{}
			secretName := apimachinerytypes.NamespacedName{
				Namespace: instance.Namespace,
				Name:      instance.Spec.Certificate.SecretName,
			}
			if err := r.client.Get(ctx, secretName, secret); err != nil {
				return "", fmt.Errorf("unable to get secret for CA certificate: %w", err)
			}
			caCertificate, ok := secret.Data["ca.crt"]
			if !ok {
				return "", fmt.Errorf("CA certificate 'ca.crt' not found in secret %s/%s", secret.Namespace, secret.Name)
			}
			return string(caCertificate), nil
		}

		if instance.Spec.Certificate.ConfigMapName != "" {
			configMap := &corev1.ConfigMap{}
			configMapName := apimachinerytypes.NamespacedName{
				Namespace: instance.Namespace,
				Name:      instance.Spec.Certificate.ConfigMapName,
			}
			if err := r.client.Get(ctx, configMapName, configMap); err != nil {
				return "", fmt.Errorf("unable to get configmap for CA certificate: %w", err)
			}
			caCertificate, ok := configMap.Data["ca.crt"]
			if !ok {
				return "", fmt.Errorf("CA certificate 'ca.crt' not found in configmap %s/%s", configMap.Namespace, configMap.Name)
			}
			return caCertificate, nil
		}
	}

	return "", nil
}
