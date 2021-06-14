package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/pkg/errors"

	"github.com/stretchr/objx"

	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/authentication/scram"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/annotations"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/secret"

	"github.com/mongodb/mongodb-kubernetes-operator/controllers/construct"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/probes"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/client"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/resourcerequirements"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func init() {
	os.Setenv(construct.AgentImageEnv, "agent-image")
}

func newTestReplicaSet() mdbv1.MongoDBCommunity {
	return mdbv1.MongoDBCommunity{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBCommunitySpec{
			Members: 3,
			Version: "4.2.2",
		},
	}
}

func newScramReplicaSet(users ...mdbv1.MongoDBUser) mdbv1.MongoDBCommunity {
	return mdbv1.MongoDBCommunity{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBCommunitySpec{
			Users:   users,
			Members: 3,
			Version: "4.2.2",
			Security: mdbv1.Security{
				Authentication: mdbv1.Authentication{
					Modes: []mdbv1.AuthMode{"SCRAM"},
				},
			},
		},
	}
}

func newTestReplicaSetWithTLS() mdbv1.MongoDBCommunity {
	return mdbv1.MongoDBCommunity{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBCommunitySpec{
			Members: 3,
			Version: "4.2.2",
			Security: mdbv1.Security{
				TLS: mdbv1.TLS{
					Enabled: true,
					CaConfigMap: mdbv1.LocalObjectReference{
						Name: "caConfigMap",
					},
					CertificateKeySecret: mdbv1.LocalObjectReference{
						Name: "certificateKeySecret",
					},
				},
			},
		},
	}
}

func TestKubernetesResources_AreCreated(t *testing.T) {
	// TODO: Create builder/yaml fixture of some type to construct MDB objects for unit tests
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)

	reconcileThroughAllStates(t, r, mdb)

	s := corev1.Secret{}
	err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace}, &s)
	assert.NoError(t, err)
	assert.Equal(t, mdb.Namespace, s.Namespace)
	assert.Equal(t, mdb.AutomationConfigSecretName(), s.Name)
	assert.Contains(t, s.Data, automationconfig.ConfigKey)
	assert.NotEmpty(t, s.Data[automationconfig.ConfigKey])
}

func TestStatefulSet_IsCorrectlyConfigured(t *testing.T) {
	_ = os.Setenv(construct.MongodbRepoUrl, "repo")
	_ = os.Setenv(construct.MongodbImageEnv, "mongo")

	mdb := newTestReplicaSet()
	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)

	reconcileThroughAllStates(t, r, mdb)

	sts := appsv1.StatefulSet{}
	err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
	assert.NoError(t, err)

	assert.Len(t, sts.Spec.Template.Spec.Containers, 2)

	agentContainer := sts.Spec.Template.Spec.Containers[1]
	assert.Equal(t, construct.AgentName, agentContainer.Name)
	assert.Equal(t, os.Getenv(construct.AgentImageEnv), agentContainer.Image)
	expectedProbe := probes.New(construct.DefaultReadiness())
	assert.True(t, reflect.DeepEqual(&expectedProbe, agentContainer.ReadinessProbe))

	mongodbContainer := sts.Spec.Template.Spec.Containers[0]
	assert.Equal(t, construct.MongodbName, mongodbContainer.Name)
	assert.Equal(t, "repo/mongo:4.2.2", mongodbContainer.Image)

	assert.Equal(t, resourcerequirements.Defaults(), agentContainer.Resources)

	acVolume, err := getVolumeByName(sts, "automation-config")
	assert.NoError(t, err)
	assert.NotNil(t, acVolume.Secret, "automation config should be stored in a secret!")
	assert.Nil(t, acVolume.ConfigMap, "automation config should be stored in a secret, not a config map!")

}

func getVolumeByName(sts appsv1.StatefulSet, volumeName string) (corev1.Volume, error) {
	for _, v := range sts.Spec.Template.Spec.Volumes {
		if v.Name == volumeName {
			return v, nil
		}
	}
	return corev1.Volume{}, errors.Errorf("volume with name %s, not found", volumeName)
}

func TestChangingVersion_ResultsInRollingUpdateStrategyType(t *testing.T) {
	mdb := newTestReplicaSet()
	mgr := client.NewManager(&mdb)
	mgrClient := mgr.GetClient()
	r := NewReconciler(mgr)

	reconcileThroughAllStates(t, r, mdb)

	_ = mgrClient.Get(context.TODO(), mdb.NamespacedName(), &mdb)

	sts := appsv1.StatefulSet{}
	err := mgrClient.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
	assert.NoError(t, err)
	assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)

	mdbRef := &mdb
	mdbRef.Spec.Version = "4.2.3"

	_ = mgrClient.Update(context.TODO(), &mdb)

	// agents start the upgrade, they are not all ready
	sts.Status.UpdatedReplicas = 1
	sts.Status.ReadyReplicas = 2
	err = mgrClient.Update(context.TODO(), &sts)
	assert.NoError(t, err)
	_ = mgrClient.Get(context.TODO(), mdb.NamespacedName(), &sts)

	reconcileThroughAllStates(t, r, mdb)

	sts = appsv1.StatefulSet{}
	err = mgrClient.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
	assert.NoError(t, err)

	assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type,
		"The StatefulSet should have be re-configured to use RollingUpdates after it reached the ready state")
}

func TestBuildStatefulSet_ConfiguresUpdateStrategyCorrectly(t *testing.T) {
	t.Run("On No Version Change, Same Version", func(t *testing.T) {
		mdb := newTestReplicaSet()
		mdb.Spec.Version = "4.0.0"
		mdb.Annotations[annotations.LastAppliedMongoDBVersion] = "4.0.0"
		sts, err := buildStatefulSet(mdb)
		assert.NoError(t, err)
		assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
	t.Run("On No Version Change, First Version", func(t *testing.T) {
		mdb := newTestReplicaSet()
		mdb.Spec.Version = "4.0.0"
		delete(mdb.Annotations, annotations.LastAppliedMongoDBVersion)
		sts, err := buildStatefulSet(mdb)
		assert.NoError(t, err)
		assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
	t.Run("On Version Change", func(t *testing.T) {
		mdb := newTestReplicaSet()

		mdb.Spec.Version = "4.0.0"

		prevSpec := mdbv1.MongoDBCommunitySpec{
			Version: "4.2.0",
		}

		bytes, err := json.Marshal(prevSpec)
		assert.NoError(t, err)

		mdb.Annotations[annotations.LastAppliedMongoDBVersion] = string(bytes)
		sts, err := buildStatefulSet(mdb)

		assert.NoError(t, err)
		assert.Equal(t, appsv1.OnDeleteStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
}

func TestService_isCorrectlyCreatedAndUpdated(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	svc := corev1.Service{}
	err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.ServiceName(), Namespace: mdb.Namespace}, &svc)
	assert.NoError(t, err)
	assert.Equal(t, svc.Spec.Type, corev1.ServiceTypeClusterIP)
	assert.Equal(t, svc.Spec.Selector["app"], mdb.ServiceName())
	assert.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, svc.Spec.Ports[0], corev1.ServicePort{Port: 27017, Name: "mongodb"})

	reconcileThroughAllStates(t, r, mdb)
}

func TestAutomationConfig_versionIsBumpedOnChange(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Equal(t, 1, currentAc.Version)

	mdb.Spec.Members++
	makeStatefulSetReady(t, mgr.GetClient(), mdb)

	_ = mgr.GetClient().Update(context.TODO(), &mdb)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err = automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Equal(t, 2, currentAc.Version)
}

func TestAutomationConfig_versionIsNotBumpedWithNoChanges(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Equal(t, currentAc.Version, 1)

	reconcileThroughAllStates(t, r, mdb)

	currentAc, err = automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Equal(t, currentAc.Version, 1)
}

func TestAutomationConfigFCVIsNotIncreasedWhenUpgradingMinorVersion(t *testing.T) {
	mdb := newTestReplicaSet()
	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Len(t, currentAc.Processes, 3)
	assert.Equal(t, currentAc.Processes[0].FeatureCompatibilityVersion, "4.2")

	// Upgrading minor version does not change the FCV on the automationConfig
	mdbRef := &mdb
	mdbRef.Spec.Version = "4.4.0"
	_ = mgr.Client.Update(context.TODO(), mdbRef)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err = automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.Len(t, currentAc.Processes, 3)
	assert.Equal(t, currentAc.Processes[0].FeatureCompatibilityVersion, "4.2")

}

func TestAutomationConfig_CustomMongodConfig(t *testing.T) {
	mdb := newTestReplicaSet()

	mongodConfig := objx.New(map[string]interface{}{})
	mongodConfig.Set("net.port", 1000)
	mongodConfig.Set("storage.other", "value")
	mongodConfig.Set("arbitrary.config.path", "value")
	mdb.Spec.AdditionalMongodConfig.Object = mongodConfig

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)

	for _, p := range currentAc.Processes {
		// Ensure port was overridden
		assert.Equal(t, float64(1000), p.Args26.Get("net.port").Data())

		// Ensure custom values were added
		assert.Equal(t, "value", p.Args26.Get("arbitrary.config.path").Data())
		assert.Equal(t, "value", p.Args26.Get("storage.other").Data())

		// Ensure default settings went unchanged
		assert.Equal(t, automationconfig.DefaultMongoDBDataDir, p.Args26.Get("storage.dbPath").Data())
		assert.Equal(t, mdb.Name, p.Args26.Get("replication.replSetName").Data())
	}
}

func TestExistingPasswordAndKeyfile_AreUsedWhenTheSecretExists(t *testing.T) {
	mdb := newScramReplicaSet()
	mgr := client.NewManager(&mdb)

	c := mgr.Client

	keyFileNsName := mdb.GetAgentKeyfileSecretNamespacedName()
	err := secret.CreateOrUpdate(c,
		secret.Builder().
			SetName(keyFileNsName.Name).
			SetNamespace(keyFileNsName.Namespace).
			SetField(scram.AgentKeyfileKey, "my-keyfile").
			Build(),
	)
	assert.NoError(t, err)

	passwordNsName := mdb.GetAgentPasswordSecretNamespacedName()
	err = secret.CreateOrUpdate(c,
		secret.Builder().
			SetName(passwordNsName.Name).
			SetNamespace(passwordNsName.Namespace).
			SetField(scram.AgentPasswordKey, "my-pass").
			Build(),
	)
	assert.NoError(t, err)

	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)
	assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
	assert.False(t, currentAc.Auth.Disabled)

	assert.Equal(t, "my-keyfile", currentAc.Auth.Key)
	assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
	assert.Equal(t, "my-pass", currentAc.Auth.AutoPwd)

}

func TestScramIsConfigured(t *testing.T) {
	assertReplicaSetIsConfiguredWithScram(t, newScramReplicaSet())
}

func TestScramIsConfiguredWhenNotSpecified(t *testing.T) {
	assertReplicaSetIsConfiguredWithScram(t, newTestReplicaSet())
}

//func TestReplicaSet_IsScaledDown_OneMember_AtATime_WhenItAlreadyExists(t *testing.T) {
//	mdb := newTestReplicaSet()
//	mdb.Spec.Members = 5
//
//	mgr := client.NewManager(&mdb)
//	r := NewReconciler(mgr)
//	res, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
//	assertReconciliationSuccessful(t, res, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//
//	assert.NoError(t, err)
//	assert.Equal(t, 5, mdb.Status.CurrentMongoDBMembers)
//
//	// scale members from five to three
//	mdb.Spec.Members = 3
//
//	err = mgr.GetClient().Update(context.TODO(), &mdb)
//	assert.NoError(t, err)
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//
//	res, err = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: mdb.NamespacedName()})
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//	assert.NoError(t, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//	assert.NoError(t, err)
//
//	assert.Equal(t, true, res.Requeue)
//	assert.Equal(t, 4, mdb.Status.CurrentMongoDBMembers)
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//
//	res, err = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: mdb.NamespacedName()})
//
//	assert.NoError(t, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//	assert.NoError(t, err)
//	assert.Equal(t, false, res.Requeue)
//	assert.Equal(t, 3, mdb.Status.CurrentMongoDBMembers)
//}

// TODO: refactor
//func TestReplicaSet_IsScaledUp_OneMember_AtATime_WhenItAlreadyExists(t *testing.T) {
//	mdb := newTestReplicaSet()
//
//	mgr := client.NewManager(&mdb)
//	r := NewReconciler(mgr)
//	res, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
//	assertReconciliationSuccessful(t, res, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//	assert.NoError(t, err)
//	assert.Equal(t, 3, mdb.Status.CurrentMongoDBMembers)
//
//	// scale members from three to five
//	mdb.Spec.Members = 5
//
//	err = mgr.GetClient().Update(context.TODO(), &mdb)
//	assert.NoError(t, err)
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//
//	res, err = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: mdb.NamespacedName()})
//
//	assert.NoError(t, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//
//	assert.NoError(t, err)
//	assert.Equal(t, true, res.Requeue)
//	assert.Equal(t, 4, mdb.Status.CurrentMongoDBMembers)
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//
//	makeStatefulSetReady(t, mgr.GetClient(), mdb)
//
//	res, err = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: mdb.NamespacedName()})
//
//	assert.NoError(t, err)
//
//	err = mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
//	assert.NoError(t, err)
//
//	assert.Equal(t, false, res.Requeue)
//	assert.Equal(t, 5, mdb.Status.CurrentMongoDBMembers)
//}

func TestIgnoreUnknownUsers(t *testing.T) {
	t.Run("Ignore Unkown Users set to true", func(t *testing.T) {
		mdb := newTestReplicaSet()
		ignoreUnknownUsers := true
		mdb.Spec.Security.Authentication.IgnoreUnknownUsers = &ignoreUnknownUsers

		assertAuthoritativeSet(t, mdb, false)
	})

	t.Run("IgnoreUnknownUsers is not set", func(t *testing.T) {
		mdb := newTestReplicaSet()
		mdb.Spec.Security.Authentication.IgnoreUnknownUsers = nil
		assertAuthoritativeSet(t, mdb, false)
	})

	t.Run("IgnoreUnknownUsers set to false", func(t *testing.T) {
		mdb := newTestReplicaSet()
		ignoreUnknownUsers := false
		mdb.Spec.Security.Authentication.IgnoreUnknownUsers = &ignoreUnknownUsers
		assertAuthoritativeSet(t, mdb, true)
	})

}

// assertAuthoritativeSet asserts that a reconciliation of the given MongoDBCommunity resource
// results in the AuthoritativeSet of the created AutomationConfig to have the expectedValue provided.
func assertAuthoritativeSet(t *testing.T, mdb mdbv1.MongoDBCommunity, expectedValue bool) {
	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	s, err := mgr.Client.GetSecret(types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	assert.NoError(t, err)

	bytes := s.Data[automationconfig.ConfigKey]
	ac, err := automationconfig.FromBytes(bytes)
	assert.NoError(t, err)

	assert.Equal(t, expectedValue, ac.Auth.AuthoritativeSet)
}

func assertReplicaSetIsConfiguredWithScram(t *testing.T, mdb mdbv1.MongoDBCommunity) {
	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	currentAc, err := automationconfig.ReadFromSecret(mgr.Client, types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace})
	t.Run("Automation Config is configured with SCRAM", func(t *testing.T) {
		assert.NotEmpty(t, currentAc.Auth.Key)
		assert.NoError(t, err)
		assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
		assert.NotEmpty(t, currentAc.Auth.AutoPwd)
		assert.False(t, currentAc.Auth.Disabled)
	})
	t.Run("Secret with password was created", func(t *testing.T) {
		secretNsName := mdb.GetAgentPasswordSecretNamespacedName()
		s, err := mgr.Client.GetSecret(secretNsName)
		assert.NoError(t, err)
		assert.Equal(t, s.Data[scram.AgentPasswordKey], []byte(currentAc.Auth.AutoPwd))
	})

	t.Run("Secret with keyfile was created", func(t *testing.T) {
		secretNsName := mdb.GetAgentKeyfileSecretNamespacedName()
		s, err := mgr.Client.GetSecret(secretNsName)
		assert.NoError(t, err)
		assert.Equal(t, s.Data[scram.AgentKeyfileKey], []byte(currentAc.Auth.Key))
	})
}

func TestReplicaSet_IsScaledUpToDesiredMembers_WhenFirstCreated(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	err := mgr.GetClient().Get(context.TODO(), mdb.NamespacedName(), &mdb)
	assert.NoError(t, err)

	assert.Equal(t, 3, mdb.Status.CurrentMongoDBMembers)
}

func TestVolumeClaimTemplates_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "volume_claim_templates_mdb.yaml")

	assert.Len(t, sts.Spec.VolumeClaimTemplates, 3)

	pvcSpec := sts.Spec.VolumeClaimTemplates[2].Spec

	storage := pvcSpec.Resources.Requests[corev1.ResourceStorage]
	storageRef := &storage

	assert.Equal(t, "1Gi", storageRef.String())
	assert.Len(t, pvcSpec.AccessModes, 1)
	assert.Contains(t, pvcSpec.AccessModes, corev1.ReadWriteOnce)
}

func TestChangeDataVolume_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "change_data_volume.yaml")
	assert.Len(t, sts.Spec.VolumeClaimTemplates, 2)

	dataVolume := sts.Spec.VolumeClaimTemplates[0]

	storage := dataVolume.Spec.Resources.Requests[corev1.ResourceStorage]
	storageRef := &storage

	assert.Equal(t, "data-volume", dataVolume.Name)
	assert.Equal(t, "50Gi", storageRef.String())
}

func TestCustomStorageClass_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "custom_storage_class.yaml")

	dataVolume := sts.Spec.VolumeClaimTemplates[0]

	storage := dataVolume.Spec.Resources.Requests[corev1.ResourceStorage]
	storageRef := &storage

	expectedStorageClass := "my-storage-class"
	expectedStorageClassRef := &expectedStorageClass

	assert.Equal(t, "data-volume", dataVolume.Name)
	assert.Equal(t, "1Gi", storageRef.String())
	assert.Equal(t, expectedStorageClassRef, dataVolume.Spec.StorageClassName)
}

func TestCustomTaintsAndTolerations_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "tolerations_example.yaml")

	assert.Len(t, sts.Spec.Template.Spec.Tolerations, 2)
	assert.Equal(t, "example-key", sts.Spec.Template.Spec.Tolerations[0].Key)
	assert.Equal(t, corev1.TolerationOpExists, sts.Spec.Template.Spec.Tolerations[0].Operator)
	assert.Equal(t, corev1.TaintEffectNoSchedule, sts.Spec.Template.Spec.Tolerations[0].Effect)

	assert.Equal(t, "example-key-2", sts.Spec.Template.Spec.Tolerations[1].Key)
	assert.Equal(t, corev1.TolerationOpEqual, sts.Spec.Template.Spec.Tolerations[1].Operator)
	assert.Equal(t, corev1.TaintEffectNoExecute, sts.Spec.Template.Spec.Tolerations[1].Effect)
}

func performReconciliationAndGetStatefulSet(t *testing.T, filePath string) appsv1.StatefulSet {
	mdb, err := loadTestFixture(filePath)
	assert.NoError(t, err)
	mgr := client.NewManager(&mdb)
	assert.NoError(t, generatePasswordsForAllUsers(mdb, mgr.Client))
	r := NewReconciler(mgr)
	reconcileThroughAllStates(t, r, mdb)

	sts, err := mgr.Client.GetStatefulSet(mdb.NamespacedName())
	assert.NoError(t, err)
	return sts
}

func generatePasswordsForAllUsers(mdb mdbv1.MongoDBCommunity, c client.Client) error {
	for _, user := range mdb.Spec.Users {

		key := "password"
		if user.PasswordSecretRef.Key != "" {
			key = user.PasswordSecretRef.Key
		}

		passwordSecret := secret.Builder().
			SetName(user.PasswordSecretRef.Name).
			SetNamespace(mdb.Namespace).
			SetField(key, "GAGTQK2ccRRaxJFudI5y").
			Build()

		if err := c.CreateSecret(passwordSecret); err != nil {
			return err
		}
	}

	return nil
}

// makeStatefulSetReady updates the StatefulSet corresponding to the
// provided MongoDB resource to mark it as ready for the case of `statefulset.IsReady`
func makeStatefulSetReady(t *testing.T, c k8sClient.Client, mdb mdbv1.MongoDBCommunity) {
	setStatefulSetReadyReplicas(t, c, mdb, mdb.StatefulSetReplicasThisReconciliation())
}

func setStatefulSetReadyReplicas(t *testing.T, c k8sClient.Client, mdb mdbv1.MongoDBCommunity, readyReplicas int) {
	sts := appsv1.StatefulSet{}
	err := c.Get(context.TODO(), mdb.NamespacedName(), &sts)
	assert.NoError(t, err)
	sts.Status.ReadyReplicas = int32(readyReplicas)
	sts.Status.UpdatedReplicas = int32(mdb.StatefulSetReplicasThisReconciliation())
	err = c.Update(context.TODO(), &sts)
	assert.NoError(t, err)
}

// loadTestFixture will create a MongoDB resource from a given fixture
func loadTestFixture(yamlFileName string) (mdbv1.MongoDBCommunity, error) {
	testPath := fmt.Sprintf("testdata/%s", yamlFileName)
	mdb := mdbv1.MongoDBCommunity{}
	data, err := ioutil.ReadFile(testPath)
	if err != nil {
		return mdb, errors.Errorf("error reading file: %s", err)
	}

	if err := marshalRuntimeObjectFromYAMLBytes(data, &mdb); err != nil {
		return mdb, errors.Errorf("error converting yaml bytes to service account: %s", err)
	}

	return mdb, nil
}

// marshalRuntimeObjectFromYAMLBytes accepts the bytes of a yaml resource
// and unmarshals them into the provided runtime Object
func marshalRuntimeObjectFromYAMLBytes(bytes []byte, obj runtime.Object) error {
	jsonBytes, err := yaml.YAMLToJSON(bytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, &obj)
}
