package keycloakrealmrole

import (
	"context"
	"testing"
	"time"

	"github.com/epam/edp-keycloak-operator/pkg/apis/v1/v1alpha1"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/adapter"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/dto"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/mock"
	"github.com/epam/edp-keycloak-operator/pkg/controller/helper"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileKeycloakRealmRole_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	ns := "security"
	keycloak := v1alpha1.Keycloak{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns},
		Spec: v1alpha1.KeycloakSpec{
			Secret: "keycloak-secret",
		},
		Status: v1alpha1.KeycloakStatus{Connected: true}}
	realm := v1alpha1.KeycloakRealm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns,
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "Keycloak"}}},
		Spec: v1alpha1.KeycloakRealmSpec{RealmName: "ns.test"}}
	now := metav1.Time{Time: time.Now()}
	role := v1alpha1.KeycloakRealmRole{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now, Name: "test-role", Namespace: ns,
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "KeycloakRealm"}}},
		Spec:   v1alpha1.KeycloakRealmRoleSpec{Name: "role-test"},
		Status: v1alpha1.KeycloakRealmRoleStatus{Value: ""},
	}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "keycloak-secret", Namespace: ns},
		Data: map[string][]byte{"username": []byte("user"), "password": []byte("pass")}}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&role, &realm, &keycloak, &secret).Build()

	kClient := new(adapter.Mock)
	kClient.On("SyncRealmRole", "ns.test",
		&dto.PrimaryRealmRole{Name: "role-test", Composites: []string{}}).Return(nil)
	kClient.On("DeleteRealmRole", "ns.test", "role-test").Return(nil)

	rkr := ReconcileKeycloakRealmRole{
		scheme: scheme,
		client: client,
		helper: helper.MakeHelper(client, scheme),
		log:    &mock.Logger{},
	}

	if _, err := rkr.Reconcile(context.TODO(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-role",
			Namespace: ns,
		},
	}); err != nil {
		t.Fatalf("%+v", err)
	}
}

func TestReconcileKeycloakRealmRole_ReconcileFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	ns := "security"
	keycloak := v1alpha1.Keycloak{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns},
		Spec: v1alpha1.KeycloakSpec{
			Secret: "keycloak-secret",
		},
		Status: v1alpha1.KeycloakStatus{Connected: true}}
	realm := v1alpha1.KeycloakRealm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns,
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "Keycloak"}}},
		TypeMeta: metav1.TypeMeta{Kind: "KeycloakRealm", APIVersion: "v1.edp.epam.com/v1alpha1"},
		Spec:     v1alpha1.KeycloakRealmSpec{RealmName: "test"}}
	role := v1alpha1.KeycloakRealmRole{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns,
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "KeycloakRealm"}}},
		TypeMeta: metav1.TypeMeta{Kind: "KeycloakRealmRole", APIVersion: "v1.edp.epam.com/v1alpha1"},
		Spec:     v1alpha1.KeycloakRealmRoleSpec{Name: "test"},
		Status:   v1alpha1.KeycloakRealmRoleStatus{Value: "unable to put role: unable to sync realm role CR: test mock fatal"},
	}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "keycloak-secret", Namespace: ns},
		Data: map[string][]byte{"username": []byte("user"), "password": []byte("pass")}}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&role, &realm, &keycloak, &secret).Build()

	mockErr := errors.New("test mock fatal")

	kClient := new(adapter.Mock)
	kClient.On("SyncRealmRole", "test",
		&dto.PrimaryRealmRole{Name: "test", Composites: []string{}}).Return(mockErr)

	h := helper.Mock{}
	logger := mock.Logger{}
	h.On("CreateKeycloakClientForRealm", &realm, &logger).Return(kClient, nil)
	h.On("GetOrCreateRealmOwnerRef", &role, role.ObjectMeta).Return(&realm, nil)
	h.On("SetFailureCount", &role).Return(time.Second)
	h.On("UpdateStatus", &role).Return(nil)

	rkr := ReconcileKeycloakRealmRole{
		scheme: scheme,
		client: client,
		helper: &h,
		log:    &logger,
	}

	_, err := rkr.Reconcile(context.TODO(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test",
			Namespace: ns,
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	loggerErr := logger.LastError()
	if loggerErr == nil {
		t.Fatal("no error on mock fatal")
	}

	if errors.Cause(loggerErr) != mockErr {
		t.Log(err)
		t.Fatal("wrong error returned")
	}
}
