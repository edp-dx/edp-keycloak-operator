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

	keycloakApi "github.com/epam/edp-keycloak-operator/api/v1"
	"github.com/epam/edp-keycloak-operator/controllers/helper"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak"
)

type Helper interface {
	CreateKeycloakClientFromAuthData(ctx context.Context, authData *helper.KeycloakAuthData, caCertificate []byte) (keycloak.Client, error)
	RetrieveCACertificate(ctx context.Context, namespace string, source helper.CertificateSource) ([]byte, error)
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
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

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

	var caCertificate []byte
	var err error

	if instance.Spec.CertificateSecret != "" || instance.Spec.CertificateConfigMap != "" {
		source := helper.CertificateSource{
			Secret:    instance.Spec.CertificateSecret,
			ConfigMap: instance.Spec.CertificateConfigMap,
		}
		caCertificate, err = r.helper.RetrieveCACertificate(ctx, req.Namespace, source)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to retrieve CA certificate: %w", err)
		}
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

func (r *ReconcileKeycloak) updateConnectionStatusToKeycloak(ctx context.Context, instance *keycloakApi.Keycloak, caCertificate []byte) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Start updating connection status to Keycloak")

	authData := helper.MakeKeycloakAuthDataFromKeycloak(instance)
	kcClient, err := r.helper.CreateKeycloakClientFromAuthData(ctx, authData, caCertificate)
	if err != nil {
		log.Error(err, "Unable to connect to Keycloak")
	}

	connected := err == nil && kcClient != nil

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