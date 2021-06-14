package controllers

import (
	"fmt"
	"go.uber.org/zap"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/secret"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// ensureUserResources will check that the configured user password secrets can be found
// and will start monitor them so that the reconcile process is triggered every time these secrets are updated
func (r ReplicaSetReconciler) ensureUserResources(mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) error {
	for _, user := range mdb.GetScramUsers() {
		log.Infof("Ensuring resources for user %s in %s", user.Username, user.Database)
		secretNamespacedName := types.NamespacedName{Name: user.PasswordSecretName, Namespace: mdb.Namespace}
		if _, err := secret.ReadKey(r.client, user.PasswordSecretKey, secretNamespacedName); err != nil {
			if apiErrors.IsNotFound(err) {
				return fmt.Errorf(`user password secret "%s" not found: %s`, secretNamespacedName, err)
			}
			return err
		}

		r.secretWatcher.Watch(secretNamespacedName, mdb.NamespacedName())
	}

	return nil
}

// updateConnectionStringSecrets updates secrets where user specific connection strings are stored.
// The client applications can mount these secrets and connect to the mongodb cluster
func (r ReplicaSetReconciler) updateConnectionStringSecrets(mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) error {
	for _, user := range mdb.GetScramUsers() {
		log.Infof("Ensuring connection string secret for user %s in %s", user.Username, user.Database)

		secretNamespacedName := types.NamespacedName{Name: user.PasswordSecretName, Namespace: mdb.Namespace}
		pwd, err := secret.ReadKey(r.client, user.PasswordSecretKey, secretNamespacedName)
		if err != nil {
			return err
		}

		connectionStringSecret := secret.Builder().
			SetName(user.GetConnectionStringSecretName(mdb)).
			SetNamespace(mdb.Namespace).
			SetField("connectionString.standard", mdb.MongoAuthUserURI(user, pwd)).
			SetField("connectionString.standardSrv", mdb.MongoAuthUserSRVURI(user, pwd)).
			SetField("username", user.Username).
			SetField("password", pwd).
			SetOwnerReferences(mdb.GetOwnerReferences()).
			Build()

		if err := secret.CreateOrUpdate(r.client, connectionStringSecret); err != nil {
			return err
		}
	}

	return nil
}
