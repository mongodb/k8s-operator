package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/pkg/errors"

	"github.com/stretchr/objx"

	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/authentication/scram"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/secret"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/probes"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/pkg/apis/mongodb/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/client"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/resourcerequirements"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	os.Setenv("AGENT_IMAGE", "agent-image")
}

func newTestReplicaSet() mdbv1.MongoDB {
	return mdbv1.MongoDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBSpec{
			Members: 3,
			Version: "4.2.2",
		},
	}
}

func newScramReplicaSet(users ...mdbv1.MongoDBUser) mdbv1.MongoDB {
	return mdbv1.MongoDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBSpec{
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

func newTestReplicaSetWithTLS() mdbv1.MongoDB {
	return mdbv1.MongoDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-rs",
			Namespace:   "my-ns",
			Annotations: map[string]string{},
		},
		Spec: mdbv1.MongoDBSpec{
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

func mockManifestProvider(version string) func() (automationconfig.VersionManifest, error) {
	return func() (automationconfig.VersionManifest, error) {
		return automationconfig.VersionManifest{
			Updated: 0,
			Versions: []automationconfig.MongoDbVersionConfig{
				{
					Name: version,
					Builds: []automationconfig.BuildConfig{{
						Platform:     "platform",
						Url:          "url",
						GitVersion:   "gitVersion",
						Architecture: "arch",
						Flavor:       "flavor",
						MinOsVersion: "0",
						MaxOsVersion: "10",
						Modules:      []string{},
					}},
				}},
		}, nil
	}
}

func TestKubernetesResources_AreCreated(t *testing.T) {
	// TODO: Create builder/yaml fixture of some type to construct MDB objects for unit tests
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))

	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	s := corev1.Secret{}
	err = mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.AutomationConfigSecretName(), Namespace: mdb.Namespace}, &s)
	assert.NoError(t, err)
	assert.Equal(t, mdb.Namespace, s.Namespace)
	assert.Equal(t, mdb.AutomationConfigSecretName(), s.Name)
	assert.Contains(t, s.Data, AutomationConfigKey)
	assert.NotEmpty(t, s.Data[AutomationConfigKey])
}

func TestStatefulSet_IsCorrectlyConfigured(t *testing.T) {
	mdb := newTestReplicaSet()
	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	sts := appsv1.StatefulSet{}
	err = mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
	assert.NoError(t, err)

	assert.Len(t, sts.Spec.Template.Spec.Containers, 2)

	agentContainer := sts.Spec.Template.Spec.Containers[0]
	assert.Equal(t, agentName, agentContainer.Name)
	assert.Equal(t, os.Getenv(agentImageEnv), agentContainer.Image)
	expectedProbe := probes.New(defaultReadiness())
	assert.True(t, reflect.DeepEqual(&expectedProbe, agentContainer.ReadinessProbe))

	mongodbContainer := sts.Spec.Template.Spec.Containers[1]
	assert.Equal(t, mongodbName, mongodbContainer.Name)
	assert.Equal(t, "mongo:4.2.2", mongodbContainer.Image)

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
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: mdb.NamespacedName()})
	assertReconciliationSuccessful(t, res, err)

	// fetch updated resource after first reconciliation
	_ = mgrClient.Get(context.TODO(), mdb.NamespacedName(), &mdb)

	sts := appsv1.StatefulSet{}
	err = mgrClient.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &sts)
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

	// the request is requeued as the agents are still doing the upgrade
	res, err = r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assert.NoError(t, err)
	assert.Equal(t, res.RequeueAfter, time.Second*10)

	_ = mgrClient.Get(context.TODO(), mdb.NamespacedName(), &sts)
	assert.Equal(t, appsv1.OnDeleteStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	// upgrade is now complete
	sts.Status.UpdatedReplicas = 3
	sts.Status.ReadyReplicas = 3
	err = mgrClient.Update(context.TODO(), &sts)
	assert.NoError(t, err)

	// reconcilliation is successful
	res, err = r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

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
		mdb.Annotations[lastVersionAnnotationKey] = "4.0.0"
		sts, err := buildStatefulSet(mdb)
		assert.NoError(t, err)
		assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
	t.Run("On No Version Change, First Version", func(t *testing.T) {
		mdb := newTestReplicaSet()
		mdb.Spec.Version = "4.0.0"
		delete(mdb.Annotations, lastVersionAnnotationKey)
		sts, err := buildStatefulSet(mdb)
		assert.NoError(t, err)
		assert.Equal(t, appsv1.RollingUpdateStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
	t.Run("On Version Change", func(t *testing.T) {
		mdb := newTestReplicaSet()
		mdb.Spec.Version = "4.0.0"
		mdb.Annotations[lastVersionAnnotationKey] = "4.2.0"
		sts, err := buildStatefulSet(mdb)
		assert.NoError(t, err)
		assert.Equal(t, appsv1.OnDeleteStatefulSetStrategyType, sts.Spec.UpdateStrategy.Type)
	})
}

func TestService_isCorrectlyCreatedAndUpdated(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	svc := corev1.Service{}
	err = mgr.GetClient().Get(context.TODO(), types.NamespacedName{Name: mdb.ServiceName(), Namespace: mdb.Namespace}, &svc)
	assert.NoError(t, err)
	assert.Equal(t, svc.Spec.Type, corev1.ServiceTypeClusterIP)
	assert.Equal(t, svc.Spec.Selector["app"], mdb.ServiceName())
	assert.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, svc.Spec.Ports[0], corev1.ServicePort{Port: 27017})

	res, err = r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)
}

func TestAutomationConfig_versionIsBumpedOnChange(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err := getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
	assert.NoError(t, err)
	assert.Equal(t, 1, currentAc.Version)

	mdb.Spec.Members++
	makeStatefulSetReady(t, mgr.GetClient(), mdb)

	_ = mgr.GetClient().Update(context.TODO(), &mdb)
	res, err = r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err = getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
	assert.NoError(t, err)
	assert.Equal(t, 2, currentAc.Version)
}

func TestAutomationConfig_versionIsNotBumpedWithNoChanges(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err := getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
	assert.NoError(t, err)
	assert.Equal(t, currentAc.Version, 1)

	res, err = r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err = getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
	assert.NoError(t, err)
	assert.Equal(t, currentAc.Version, 1)
}

func TestAutomationConfig_CustomMongodConfig(t *testing.T) {
	mdb := newTestReplicaSet()

	mongodConfig := objx.New(map[string]interface{}{})
	mongodConfig.Set("net.port", 1000)
	mongodConfig.Set("storage.other", "value")
	mongodConfig.Set("arbitrary.config.path", "value")
	mdb.Spec.AdditionalMongodConfig.Object = mongodConfig

	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err := getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
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

	scramNsName := mdb.ScramCredentialsNamespacedName()
	err := secret.CreateOrUpdate(c,
		secret.Builder().
			SetName(scramNsName.Name).
			SetNamespace(scramNsName.Namespace).
			SetField(scram.AgentPasswordKey, "my-pass").
			SetField(scram.AgentKeyfileKey, "my-keyfile").
			Build(),
	)
	assert.NoError(t, err)

	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err := getCurrentAutomationConfig(c, mdb)
	assert.NoError(t, err)
	assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
	assert.False(t, currentAc.Auth.Disabled)

	assert.Equal(t, "my-keyfile", currentAc.Auth.Key)
	assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
	assert.Equal(t, "my-pass", currentAc.Auth.AutoPwd)

}

func TestScramIsConfigured(t *testing.T) {
	AssertReplicaSetIsConfiguredWithScram(t, newScramReplicaSet())
}

func TestScramIsConfiguredWhenNotSpecified(t *testing.T) {
	AssertReplicaSetIsConfiguredWithScram(t, newTestReplicaSet())
}

func AssertReplicaSetIsConfiguredWithScram(t *testing.T, mdb mdbv1.MongoDB) {
	mgr := client.NewManager(&mdb)
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assertReconciliationSuccessful(t, res, err)

	currentAc, err := getCurrentAutomationConfig(client.NewClient(mgr.GetClient()), mdb)
	t.Run("Automation Config is configured with SCRAM", func(t *testing.T) {
		assert.NoError(t, err)
		assert.NotEmpty(t, currentAc.Auth.Key)
		assert.NotEmpty(t, currentAc.Auth.KeyFileWindows)
		assert.NotEmpty(t, currentAc.Auth.AutoPwd)
		assert.False(t, currentAc.Auth.Disabled)
	})
	t.Run("Secret with credentials was created", func(t *testing.T) {
		secretNsName := mdb.ScramCredentialsNamespacedName()
		s, err := mgr.Client.GetSecret(secretNsName)
		assert.NoError(t, err)
		assert.Equal(t, s.Data[scram.AgentKeyfileKey], []byte(currentAc.Auth.Key))
		assert.Equal(t, s.Data[scram.AgentPasswordKey], []byte(currentAc.Auth.AutoPwd))
	})
}

func TestOpenshift_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "openshift_mdb.yaml")
	assert.Equal(t, "MANAGED_SECURITY_CONTEXT", sts.Spec.Template.Spec.Containers[0].Env[1].Name)
	assert.Equal(t, "MANAGED_SECURITY_CONTEXT", sts.Spec.Template.Spec.Containers[1].Env[1].Name)
}

func TestVolumeClaimTemplates_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "volume_claim_templates_mdb.yaml")

	assert.Len(t, sts.Spec.VolumeClaimTemplates, 2)

	pvcSpec := sts.Spec.VolumeClaimTemplates[1].Spec

	storage := pvcSpec.Resources.Requests[corev1.ResourceStorage]
	storageRef := &storage

	assert.Equal(t, "1Gi", storageRef.String())
	assert.Len(t, pvcSpec.AccessModes, 1)
	assert.Contains(t, pvcSpec.AccessModes, corev1.ReadWriteOnce)
}

func TestChangeDataVolume_Configuration(t *testing.T) {
	sts := performReconciliationAndGetStatefulSet(t, "change_data_volume.yaml")
	assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)

	dataVolume := sts.Spec.VolumeClaimTemplates[0]

	storage := dataVolume.Spec.Resources.Requests[corev1.ResourceStorage]
	storageRef := &storage

	assert.Equal(t, "data-volume", dataVolume.Name)
	assert.Equal(t, "50Gi", storageRef.String())
}

func performReconciliationAndGetStatefulSet(t *testing.T, filePath string) appsv1.StatefulSet {
	mdb, err := loadTestFixture(filePath)
	assert.NoError(t, err)
	mgr := client.NewManager(&mdb)
	assert.NoError(t, generatePasswordsForAllUsers(mdb, mgr.Client))
	r := newReconciler(mgr, mockManifestProvider(mdb.Spec.Version))
	res, err := r.Reconcile(reconcile.Request{NamespacedName: mdb.NamespacedName()})
	assertReconciliationSuccessful(t, res, err)

	sts, err := mgr.Client.GetStatefulSet(mdb.NamespacedName())
	assert.NoError(t, err)
	return sts
}

func generatePasswordsForAllUsers(mdb mdbv1.MongoDB, c client.Client) error {
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

func assertReconciliationSuccessful(t *testing.T, result reconcile.Result, err error) {
	assert.NoError(t, err)
	assert.Equal(t, false, result.Requeue)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}

// makeStatefulSetReady updates the StatefulSet corresponding to the
// provided MongoDB resource to mark it as ready for the case of `statefulset.IsReady`
func makeStatefulSetReady(t *testing.T, c k8sClient.Client, mdb mdbv1.MongoDB) {
	sts := appsv1.StatefulSet{}
	err := c.Get(context.TODO(), mdb.NamespacedName(), &sts)
	assert.NoError(t, err)
	sts.Status.ReadyReplicas = int32(mdb.Spec.Members)
	sts.Status.UpdatedReplicas = int32(mdb.Spec.Members)
	err = c.Update(context.TODO(), &sts)
	assert.NoError(t, err)
}

// loadTestFixture will create a MongoDB resource from a given fixture
func loadTestFixture(yamlFileName string) (mdbv1.MongoDB, error) {
	testPath := fmt.Sprintf("testdata/%s", yamlFileName)
	mdb := mdbv1.MongoDB{}
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
