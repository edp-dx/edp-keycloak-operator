package keycloakrealm

import (
	"context"
	"fmt"

	v1v1alpha1 "github.com/epmd-edp/keycloak-operator/pkg/apis/v1/v1alpha1"
	"github.com/epmd-edp/keycloak-operator/pkg/client/keycloak"
	"github.com/epmd-edp/keycloak-operator/pkg/client/keycloak/adapter"
	"github.com/epmd-edp/keycloak-operator/pkg/client/keycloak/dto"
	"github.com/epmd-edp/keycloak-operator/pkg/controller/helper"
	"github.com/epmd-edp/keycloak-operator/pkg/controller/keycloakrealm/chain"
	rHand "github.com/epmd-edp/keycloak-operator/pkg/controller/keycloakrealm/chain/handler"
	"github.com/pkg/errors"
	coreV1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	keyCloakRealmOperatorFinalizerName = "keycloak.realm.operator.finalizer.name"
)

var log = logf.Log.WithName("controller_keycloakrealm")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new KeycloakRealm Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileKeycloakRealm{
		client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		factory: new(adapter.GoCloakAdapterFactory),
		handler: chain.CreateDefChain(mgr.GetClient(), mgr.GetScheme()),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("keycloakrealm-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource KeycloakRealm
	return c.Watch(&source.Kind{Type: &v1v1alpha1.KeycloakRealm{}}, &handler.EnqueueRequestForObject{})
}

// blank assignment to verify that ReconcileKeycloakRealm implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileKeycloakRealm{}

// ReconcileKeycloakRealm reconciles a KeycloakRealm object
type ReconcileKeycloakRealm struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	scheme  *runtime.Scheme
	factory keycloak.ClientFactory
	handler rHand.RealmHandler
}

// Reconcile reads that state of the cluster for a KeycloakRealm object and makes changes based on the state read
// and what is in the KeycloakRealm.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileKeycloakRealm) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling KeycloakRealm")

	// Fetch the KeycloakRealm instance
	instance := &v1v1alpha1.KeycloakRealm{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	defer r.updateStatus(instance)

	err = r.tryReconcile(instance)
	instance.Status.Available = err == nil

	return reconcile.Result{}, err
}

func (r *ReconcileKeycloakRealm) updateStatus(kr *v1v1alpha1.KeycloakRealm) {
	err := r.client.Status().Update(context.TODO(), kr)
	if err != nil {
		_ = r.client.Update(context.TODO(), kr)
	}
}

func (r *ReconcileKeycloakRealm) tryToDelete(realm *v1v1alpha1.KeycloakRealm, kClient keycloak.Client) (bool, error) {
	if realm.GetDeletionTimestamp().IsZero() {
		if !helper.ContainsString(realm.ObjectMeta.Finalizers, keyCloakRealmOperatorFinalizerName) {
			realm.ObjectMeta.Finalizers = append(realm.ObjectMeta.Finalizers,
				keyCloakRealmOperatorFinalizerName)
			if err := r.client.Update(context.TODO(), realm); err != nil {
				return false, errors.Wrap(err, "unable to update kk realm cr")
			}
		}

		return false, nil
	}

	reqLog := log.WithValues("keycloak realm cr", realm)
	reqLog.Info("Start deleting keycloak realm...")

	if err := kClient.DeleteRealm(realm.Spec.RealmName); err != nil {
		return false, errors.Wrap(err, "unable to delete realm")
	}

	reqLog.Info("client deletion done")

	realm.ObjectMeta.Finalizers = helper.RemoveString(realm.ObjectMeta.Finalizers,
		keyCloakRealmOperatorFinalizerName)
	if err := r.client.Update(context.TODO(), realm); err != nil {
		return false, errors.Wrap(err, "unable to update kk cr")
	}

	return true, nil
}

func (r *ReconcileKeycloakRealm) tryReconcile(realm *v1v1alpha1.KeycloakRealm) error {
	c, err := r.createKeycloakClient(realm)
	if err != nil {
		return err
	}

	deleted, err := r.tryToDelete(realm, c)
	if err != nil {
		return errors.Wrap(err, "error during realm deletion")
	}
	if deleted {
		return nil
	}

	if err := r.handler.ServeRequest(realm, c); err != nil {
		return errors.Wrap(err, "error during realm chain")
	}

	return nil
}

func (r *ReconcileKeycloakRealm) createKeycloakClient(realm *v1v1alpha1.KeycloakRealm) (keycloak.Client, error) {
	o, err := r.getOrCreateKeycloakOwnerRef(realm)
	if err != nil {
		return nil, err
	}
	if !o.Status.Connected {
		return nil, errors.New("Owner keycloak is not in connected status")
	}
	s := &coreV1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      o.Spec.Secret,
		Namespace: o.Namespace,
	}, s)
	if err != nil {
		return nil, err
	}
	user := string(s.Data["username"])
	pwd := string(s.Data["password"])
	return r.factory.New(dto.ConvertSpecToKeycloak(o.Spec, user, pwd))
}

func (r *ReconcileKeycloakRealm) getOrCreateKeycloakOwnerRef(realm *v1v1alpha1.KeycloakRealm) (*v1v1alpha1.Keycloak, error) {
	o, err := helper.GetOwnerKeycloak(r.client, realm.ObjectMeta)
	if err != nil {
		return nil, err
	}
	if o != nil {
		return o, nil
	}
	if realm.Spec.KeycloakOwner == "" {
		return nil, fmt.Errorf("keycloak owner is not specified neither in ownerReference nor in spec for realm %s",
			realm.Name)
	}
	k := &v1v1alpha1.Keycloak{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: realm.Namespace,
		Name:      realm.Spec.KeycloakOwner,
	}, k)
	if err != nil {
		return nil, err
	}
	return k, controllerutil.SetControllerReference(k, realm, r.scheme)
}
