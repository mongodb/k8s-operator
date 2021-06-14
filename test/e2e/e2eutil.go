package e2eutil

import (
	"context"
	"fmt"
	"reflect"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/envvar"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const testDataDirEnv = "TEST_DATA_DIR"

func TestDataDir() string {
	return envvar.GetEnvOrDefault(testDataDirEnv, "/workspace/testdata")
}

func TlsTestDataDir() string {
	return fmt.Sprintf("%s/tls", TestDataDir())
}

// UpdateMongoDBResource applies the provided function to the most recent version of the MongoDB resource
// and retries when there are conflicts
func UpdateMongoDBResource(original *mdbv1.MongoDBCommunity, updateFunc func(*mdbv1.MongoDBCommunity)) error {
	err := TestClient.Get(context.TODO(), types.NamespacedName{Name: original.Name, Namespace: original.Namespace}, original)
	if err != nil {
		return err
	}

	updateFunc(original)

	return TestClient.Update(context.TODO(), original)
}

func NewTestMongoDB(ctx *Context, name string, namespace string) (mdbv1.MongoDBCommunity, mdbv1.MongoDBUser) {
	mongodbNamespace := namespace
	if mongodbNamespace == "" {
		mongodbNamespace = OperatorNamespace
	}
	mdb := mdbv1.MongoDBCommunity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: mongodbNamespace,
		},
		Spec: mdbv1.MongoDBCommunitySpec{
			Members: 3,
			Type:    "ReplicaSet",
			Version: "4.4.0",
			Security: mdbv1.Security{
				Authentication: mdbv1.Authentication{
					Modes: []mdbv1.AuthMode{"SCRAM"},
				},
			},
			Users: []mdbv1.MongoDBUser{
				{
					Name: fmt.Sprintf("%s-user", name),
					DB:   "admin",
					PasswordSecretRef: mdbv1.SecretKeyReference{
						Key:  fmt.Sprintf("%s-password", name),
						Name: fmt.Sprintf("%s-%s-password-secret", name, ctx.ExecutionId),
					},
					Roles: []mdbv1.Role{
						// roles on testing db for general connectivity
						{
							DB:   "testing",
							Name: "readWrite",
						},
						{
							DB:   "testing",
							Name: "clusterAdmin",
						},
						// admin roles for reading FCV
						{
							DB:   "admin",
							Name: "readWrite",
						},
						{
							DB:   "admin",
							Name: "clusterAdmin",
						},
						{
							DB:   "admin",
							Name: "userAdmin",
						},
					},
					ScramCredentialsSecretName: fmt.Sprintf("%s-my-scram", name),
				},
			},
		},
	}
	return mdb, mdb.Spec.Users[0]
}

func NewTestTLSConfig(optional bool) mdbv1.TLS {
	return mdbv1.TLS{
		Enabled:  true,
		Optional: optional,
		CertificateKeySecret: mdbv1.LocalObjectReference{
			Name: "test-tls-secret",
		},
		CaConfigMap: mdbv1.LocalObjectReference{
			Name: "test-tls-ca",
		},
	}
}

func ensureObject(ctx *Context, obj k8sClient.Object) error {
	key := k8sClient.ObjectKeyFromObject(obj)

	err := TestClient.Get(context.TODO(), key, obj)
	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return err
		}
		err = TestClient.Create(context.TODO(), obj, &CleanupOptions{TestContext: ctx})
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("%s %s/%s already exists!\n", reflect.TypeOf(obj), key.Namespace, key.Name)
		err = TestClient.Update(context.TODO(), obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// EnsureNamespace checks that the given namespace exists and creates it if not.
func EnsureNamespace(ctx *Context, namespace string) error {
	return ensureObject(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	})
}

// EnsureServiceAccount checks that the given ServiceAccount exists and creates it if not.
func EnsureServiceAccount(ctx *Context, namespace string, svcAcctName string) error {
	return ensureObject(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcAcctName,
			Namespace: namespace,
		},
	})
}

// EnsureRole checks that the given role exists and creates it with the given rules if not.
func EnsureRole(ctx *Context, namespace string, roleName string, rules []rbacv1.PolicyRule) error {
	return ensureObject(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      roleName,
		},
		Rules: rules,
	})
}

// EnsureClusterRole checks that the given cluster role exists and creates it with the given rules if not.
func EnsureClusterRole(ctx *Context, namespace string, roleName string, rules []rbacv1.PolicyRule) error {
	return ensureObject(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      roleName,
		},
		Rules: rules,
	})
}

// EnsureRoleBinding checks that the given role binding exists and creates it with the given subjects and roleRef if not.
func EnsureRoleBinding(ctx *Context, namespace string, roleBindingName string, subjects []rbacv1.Subject, roleRef rbacv1.RoleRef) error {
	return ensureObject(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      roleBindingName,
		},
		Subjects: subjects,
		RoleRef:  roleRef,
	})
}

// EnsureClusterRoleBinding checks that the given cluster role exists and creates it with the given subjects and roleRef if not.
func EnsureClusterRoleBinding(ctx *Context, namespace string, roleBindingName string, subjects []rbacv1.Subject, roleRef rbacv1.RoleRef) error {
	return ensureObject(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      roleBindingName,
		},
		Subjects: subjects,
		RoleRef:  roleRef,
	})
}
