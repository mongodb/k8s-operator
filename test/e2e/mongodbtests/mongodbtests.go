package mongodbtests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/container"
	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/util/wait"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/authentication/scram"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"
	e2eutil "github.com/mongodb/mongodb-kubernetes-operator/test/e2e"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// SkipTestIfLocal skips tests locally which tests connectivity to mongodb pods
func SkipTestIfLocal(t *testing.T, msg string, f func(t *testing.T)) {
	if testing.Short() {
		t.Log("Skipping [" + msg + "]")
		return
	}
	t.Run(msg, f)
}

// StatefulSetBecomesReady ensures that the underlying stateful set
// reaches the running state.
func StatefulSetBecomesReady(mdb *mdbv1.MongoDBCommunity, opts ...wait.Configuration) func(t *testing.T) {
	defaultOpts := []wait.Configuration{
		wait.RetryInterval(time.Second * 15),
		wait.Timeout(time.Minute * 15),
	}
	defaultOpts = append(defaultOpts, opts...)
	return statefulSetIsReady(mdb, defaultOpts...)
}

// StatefulSetBecomesUnready ensures the underlying stateful set reaches
// the unready state.
func StatefulSetBecomesUnready(mdb *mdbv1.MongoDBCommunity, opts ...wait.Configuration) func(t *testing.T) {
	defaultOpts := []wait.Configuration{
		wait.RetryInterval(time.Second * 15),
		wait.Timeout(time.Minute * 15),
	}
	defaultOpts = append(defaultOpts, opts...)
	return statefulSetIsNotReady(mdb, defaultOpts...)
}

// StatefulSetIsReadyAfterScaleDown ensures that a replica set is scaled down correctly
// note: scaling down takes considerably longer than scaling up due the readiness probe
// failure threshold being high
func StatefulSetIsReadyAfterScaleDown(mdb *mdbv1.MongoDBCommunity) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForStatefulSetToBeReadyAfterScaleDown(t, mdb, wait.RetryInterval(time.Second*60), wait.Timeout(time.Minute*45))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("StatefulSet %s/%s is ready!", mdb.Namespace, mdb.Name)
	}
}

// StatefulSetIsReady ensures that the underlying stateful set
// reaches the running state
func statefulSetIsReady(mdb *mdbv1.MongoDBCommunity, opts ...wait.Configuration) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForStatefulSetToBeReady(t, mdb, opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("StatefulSet %s/%s is ready!", mdb.Namespace, mdb.Name)
	}
}

// statefulSetIsNotReady ensures that the underlying stateful set reaches the unready state.
func statefulSetIsNotReady(mdb *mdbv1.MongoDBCommunity, opts ...wait.Configuration) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForStatefulSetToBeUnready(t, mdb, opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("StatefulSet %s/%s is not ready!", mdb.Namespace, mdb.Name)
	}
}

func StatefulSetHasOwnerReference(mdb *mdbv1.MongoDBCommunity, expectedOwnerReference metav1.OwnerReference) func(t *testing.T) {
	return func(t *testing.T) {
		stsNamespacedName := types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}
		sts := appsv1.StatefulSet{}
		err := e2eutil.TestClient.Get(context.TODO(), stsNamespacedName, &sts)

		if err != nil {
			t.Fatal(err)
		}
		assertEqualOwnerReference(t, "StatefulSet", stsNamespacedName, sts.GetOwnerReferences(), expectedOwnerReference)
	}
}

func ServiceHasOwnerReference(mdb *mdbv1.MongoDBCommunity, expectedOwnerReference metav1.OwnerReference) func(t *testing.T) {
	return func(t *testing.T) {
		serviceNamespacedName := types.NamespacedName{Name: mdb.ServiceName(), Namespace: mdb.Namespace}
		srv := corev1.Service{}
		err := e2eutil.TestClient.Get(context.TODO(), serviceNamespacedName, &srv)
		if err != nil {
			t.Fatal(err)
		}
		assertEqualOwnerReference(t, "Service", serviceNamespacedName, srv.GetOwnerReferences(), expectedOwnerReference)
	}
}

func AgentSecretsHaveOwnerReference(mdb *mdbv1.MongoDBCommunity, expectedOwnerReference metav1.OwnerReference) func(t *testing.T) {
	checkSecret := func(t *testing.T, resourceNamespacedName types.NamespacedName) {
		secret := corev1.Secret{}
		err := e2eutil.TestClient.Get(context.TODO(), resourceNamespacedName, &secret)

		assert.NoError(t, err)
		assertEqualOwnerReference(t, "Secret", resourceNamespacedName, secret.GetOwnerReferences(), expectedOwnerReference)
	}

	return func(t *testing.T) {
		checkSecret(t, mdb.GetAgentPasswordSecretNamespacedName())
		checkSecret(t, mdb.GetAgentKeyfileSecretNamespacedName())
	}
}

// ConnectionStringSecretsAreConfigured verifies that secrets storing the connection string were generated for all scram users
// and that they have the expected owner reference
func ConnectionStringSecretsAreConfigured(mdb *mdbv1.MongoDBCommunity, expectedOwnerReference metav1.OwnerReference) func(t *testing.T) {
	return func(t *testing.T) {
		for _, user := range mdb.GetScramUsers() {
			secret := corev1.Secret{}
			secretNamespacedName := types.NamespacedName{Name: user.GetConnectionStringSecretName(mdb), Namespace: mdb.Namespace}
			err := e2eutil.TestClient.Get(context.TODO(), secretNamespacedName, &secret)

			assert.NoError(t, err)
			assertEqualOwnerReference(t, "Secret", secretNamespacedName, secret.GetOwnerReferences(), expectedOwnerReference)
		}
	}
}

// StatefulSetHasUpdateStrategy verifies that the StatefulSet holding this MongoDB
// resource has the correct Update Strategy
func StatefulSetHasUpdateStrategy(mdb *mdbv1.MongoDBCommunity, strategy appsv1.StatefulSetUpdateStrategyType) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForStatefulSetToHaveUpdateStrategy(t, mdb, strategy, wait.RetryInterval(time.Second*15), wait.Timeout(time.Minute*8))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("StatefulSet %s/%s is ready!", mdb.Namespace, mdb.Name)
	}
}

// MongoDBReachesRunningPhase ensure the MongoDB resource reaches the Running phase
func MongoDBReachesRunningPhase(mdb *mdbv1.MongoDBCommunity) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForMongoDBToReachPhase(t, mdb, mdbv1.Running, time.Second*15, time.Minute*12)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MongoDB %s/%s is Running!", mdb.Namespace, mdb.Name)
	}
}

// MongoDBReachesFailed ensure the MongoDB resource reaches the Failed phase.
func MongoDBReachesFailedPhase(mdb *mdbv1.MongoDBCommunity) func(t *testing.T) {
	return func(t *testing.T) {
		err := wait.ForMongoDBToReachPhase(t, mdb, mdbv1.Failed, time.Second*15, time.Minute*5)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MongoDB %s/%s is in Failed state!", mdb.Namespace, mdb.Name)
	}
}

func AutomationConfigSecretExists(mdb *mdbv1.MongoDBCommunity) func(t *testing.T) {
	return func(t *testing.T) {
		s, err := wait.ForSecretToExist(mdb.AutomationConfigSecretName(), time.Second*5, time.Minute*1, mdb.Namespace)
		assert.NoError(t, err)

		t.Logf("Secret %s/%s was successfully created", mdb.AutomationConfigSecretName(), mdb.Namespace)
		assert.Contains(t, s.Data, automationconfig.ConfigKey)

		t.Log("The Secret contained the automation config")
	}
}

func getAutomationConfig(t *testing.T, mdb *mdbv1.MongoDBCommunity) automationconfig.AutomationConfig {
	currentSecret := corev1.Secret{}
	currentAc := automationconfig.AutomationConfig{}
	err := e2eutil.TestClient.Get(context.TODO(), types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace}, &currentSecret)
	assert.NoError(t, err)
	err = json.Unmarshal(currentSecret.Data[automationconfig.ConfigKey], &currentAc)
	assert.NoError(t, err)
	return currentAc
}

// AutomationConfigVersionHasTheExpectedVersion verifies that the automation config has the expected version.
func AutomationConfigVersionHasTheExpectedVersion(mdb *mdbv1.MongoDBCommunity, expectedVersion int) func(t *testing.T) {
	return func(t *testing.T) {
		currentAc := getAutomationConfig(t, mdb)
		assert.Equal(t, expectedVersion, currentAc.Version)
	}
}

// AutomationConfigHasTheExpectedCustomRoles verifies that the automation config has the expected custom roles.
func AutomationConfigHasTheExpectedCustomRoles(mdb *mdbv1.MongoDBCommunity, roles []automationconfig.CustomRole) func(t *testing.T) {
	return func(t *testing.T) {
		currentAc := getAutomationConfig(t, mdb)
		assert.ElementsMatch(t, roles, currentAc.Roles)
	}
}

// CreateMongoDBResource creates the MongoDB resource
func CreateMongoDBResource(mdb *mdbv1.MongoDBCommunity, ctx *e2eutil.Context) func(*testing.T) {
	return func(t *testing.T) {
		if err := e2eutil.TestClient.Create(context.TODO(), mdb, &e2eutil.CleanupOptions{TestContext: ctx}); err != nil {
			t.Fatal(err)
		}
		t.Logf("Created MongoDB resource %s/%s", mdb.Name, mdb.Namespace)
	}
}

// GetConnectionStringSecret returnes the secret generated by the operator that is storing the connection string for a specific user
func GetConnectionStringSecret(mdb mdbv1.MongoDBCommunity, user scram.User) corev1.Secret {
	secret := corev1.Secret{}
	secretNamespacedName := types.NamespacedName{Name: user.GetConnectionStringSecretName(mdb), Namespace: mdb.Namespace}
	_ = e2eutil.TestClient.Get(context.TODO(), secretNamespacedName, &secret)
	return secret
}

// GetConnectionStringForUser returns the mongodb standard connection string for a user
func GetConnectionStringForUser(mdb mdbv1.MongoDBCommunity, user scram.User) string {
	return string(GetConnectionStringSecret(mdb, user).Data["connectionString.standard"])
}

// GetConnectionStringForUser returns the mongodb service connection string for a user
func GetSrvConnectionStringForUser(mdb mdbv1.MongoDBCommunity, user scram.User) string {
	return string(GetConnectionStringSecret(mdb, user).Data["connectionString.standardSrv"])
}

func getOwnerReference(mdb *mdbv1.MongoDBCommunity) metav1.OwnerReference {
	return *metav1.NewControllerRef(mdb, schema.GroupVersionKind{
		Group:   mdbv1.GroupVersion.Group,
		Version: mdbv1.GroupVersion.Version,
		Kind:    mdb.Kind,
	})
}

func BasicFunctionality(mdb *mdbv1.MongoDBCommunity) func(*testing.T) {
	return func(t *testing.T) {
		mdbOwnerReference := getOwnerReference(mdb)
		t.Run("Secret Was Correctly Created", AutomationConfigSecretExists(mdb))
		t.Run("Stateful Set Reaches Ready State", StatefulSetBecomesReady(mdb))
		t.Run("MongoDB Reaches Running Phase", MongoDBReachesRunningPhase(mdb))
		t.Run("Stateful Set Has OwnerReference", StatefulSetHasOwnerReference(mdb, mdbOwnerReference))
		t.Run("Service Set Has OwnerReference", ServiceHasOwnerReference(mdb, mdbOwnerReference))
		t.Run("Agent Secrets Have OwnerReference", AgentSecretsHaveOwnerReference(mdb, mdbOwnerReference))
		t.Run("Connection string secrets are configured", ConnectionStringSecretsAreConfigured(mdb, mdbOwnerReference))
		t.Run("Test Status Was Updated", Status(mdb,
			mdbv1.MongoDBCommunityStatus{
				MongoURI:                   mdb.MongoURI(),
				Phase:                      mdbv1.Running,
				CurrentMongoDBMembers:      mdb.Spec.Members,
				CurrentStatefulSetReplicas: mdb.Spec.Members,
			}))
	}
}

// DeletePod will delete a pod that belongs to this MongoDB resource's StatefulSet
func DeletePod(mdb *mdbv1.MongoDBCommunity, podNum int) func(*testing.T) {
	return func(t *testing.T) {
		pod := podFromMongoDBCommunity(mdb, podNum)
		if err := e2eutil.TestClient.Delete(context.TODO(), &pod); err != nil {
			t.Fatal(err)
		}

		t.Logf("pod %s/%s deleted", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
	}
}

// Status compares the given status to the actual status of the MongoDB resource
func Status(mdb *mdbv1.MongoDBCommunity, expectedStatus mdbv1.MongoDBCommunityStatus) func(t *testing.T) {
	return func(t *testing.T) {
		if err := e2eutil.TestClient.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, mdb); err != nil {
			t.Fatalf("error getting MongoDB resource: %s", err)
		}
		assert.Equal(t, expectedStatus, mdb.Status)
	}
}

// Scale update the MongoDB with a new number of members and updates the resource
func Scale(mdb *mdbv1.MongoDBCommunity, newMembers int) func(*testing.T) {
	return func(t *testing.T) {
		t.Logf("Scaling Mongodb %s, to %d members", mdb.Name, newMembers)
		err := e2eutil.UpdateMongoDBResource(mdb, func(db *mdbv1.MongoDBCommunity) {
			db.Spec.Members = newMembers
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// DisableTLS changes the tls.enabled attribute to false.
func DisableTLS(mdb *mdbv1.MongoDBCommunity) func(*testing.T) {
	return tls(mdb, false)
}

// EnableTLS changes the tls.enabled attribute to true.
func EnableTLS(mdb *mdbv1.MongoDBCommunity) func(*testing.T) {
	return tls(mdb, true)
}

// tls function configures the security.tls.enabled attribute.
func tls(mdb *mdbv1.MongoDBCommunity, enabled bool) func(*testing.T) {
	return func(t *testing.T) {
		t.Logf("Setting security.tls.enabled to %t", enabled)
		err := e2eutil.UpdateMongoDBResource(mdb, func(db *mdbv1.MongoDBCommunity) {
			db.Spec.Security.TLS.Enabled = enabled
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func ChangeVersion(mdb *mdbv1.MongoDBCommunity, newVersion string) func(*testing.T) {
	return func(t *testing.T) {
		t.Logf("Changing versions from: %s to %s", mdb.Spec.Version, newVersion)
		err := e2eutil.UpdateMongoDBResource(mdb, func(db *mdbv1.MongoDBCommunity) {
			db.Spec.Version = newVersion
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func StatefulSetContainerConditionIsTrue(mdb *mdbv1.MongoDBCommunity, containerName string, condition func(c corev1.Container) bool) func(*testing.T) {
	return func(t *testing.T) {
		sts := appsv1.StatefulSet{}
		err := e2eutil.TestClient.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
		if err != nil {
			t.Fatal(err)
		}

		existingContainer := container.GetByName(containerName, sts.Spec.Template.Spec.Containers)
		if existingContainer == nil {
			t.Fatalf(`No container found with name "%s" in StatefulSet pod template`, containerName)
		}

		if !condition(*existingContainer) {
			t.Fatalf(`Container "%s" does not satisfy condition`, containerName)
		}
	}
}

// PodContainerBecomesReady waits until the container with 'containerName' in the pod #podNum becomes not ready.
func PodContainerBecomesNotReady(mdb *mdbv1.MongoDBCommunity, podNum int, containerName string) func(*testing.T) {
	return func(t *testing.T) {
		pod := podFromMongoDBCommunity(mdb, podNum)
		assert.NoError(t, wait.ForPodReadiness(t, false, containerName, time.Minute*10, pod))
	}
}

// PodContainerBecomesReady waits until the container with 'containerName' in the pod #podNum becomes ready.
func PodContainerBecomesReady(mdb *mdbv1.MongoDBCommunity, podNum int, containerName string) func(*testing.T) {
	return func(t *testing.T) {
		pod := podFromMongoDBCommunity(mdb, podNum)
		assert.NoError(t, wait.ForPodReadiness(t, true, containerName, time.Minute*3, pod))
	}
}

func ExecInContainer(mdb *mdbv1.MongoDBCommunity, podNum int, containerName, command string) func(*testing.T) {
	return func(t *testing.T) {
		pod := podFromMongoDBCommunity(mdb, podNum)
		_, err := e2eutil.TestClient.Execute(pod, containerName, command)
		assert.NoError(t, err)
	}
}

func podFromMongoDBCommunity(mdb *mdbv1.MongoDBCommunity, podNum int) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", mdb.Name, podNum),
			Namespace: mdb.Namespace,
		},
	}
}

func assertEqualOwnerReference(t *testing.T, resourceType string, resourceNamespacedName types.NamespacedName, ownerReferences []metav1.OwnerReference, expectedOwnerReference metav1.OwnerReference) {
	assert.Len(t, ownerReferences, 1, fmt.Sprintf("%s %s/%s doesn't have OwnerReferences", resourceType, resourceNamespacedName.Name, resourceNamespacedName.Namespace))

	assert.Equal(t, expectedOwnerReference.APIVersion, ownerReferences[0].APIVersion)
	assert.Equal(t, "MongoDBCommunity", ownerReferences[0].Kind)
	assert.Equal(t, expectedOwnerReference.Name, ownerReferences[0].Name)
	assert.Equal(t, expectedOwnerReference.UID, ownerReferences[0].UID)
}
