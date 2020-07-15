package mongodb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/persistentvolumeclaim"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/probes"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/container"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/podtemplatespec"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/controller/predicates"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/pkg/apis/mongodb/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"
	mdbClient "github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/client"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/configmap"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/resourcerequirements"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/service"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/statefulset"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	agentImageEnv                = "AGENT_IMAGE"
	versionUpgradeHookImageEnv   = "VERSION_UPGRADE_HOOK_IMAGE"
	agentHealthStatusFilePathEnv = "AGENT_STATUS_FILEPATH"

	AutomationConfigKey            = "automation-config"
	agentName                      = "mongodb-agent"
	mongodbName                    = "mongod"
	versionUpgradeHookName         = "mongod-posthook"
	dataVolumeName                 = "data-volume"
	versionManifestFilePath        = "/usr/local/version_manifest.json"
	readinessProbePath             = "/var/lib/mongodb-mms-automation/probes/readinessprobe"
	clusterFilePath                = "/var/lib/automation/config/automation-config"
	operatorServiceAccountName     = "mongodb-kubernetes-operator"
	agentHealthStatusFilePathValue = "/var/log/mongodb-mms-automation/healthstatus/agent-health-status.json"

	tlsCAMountPath     = "/var/lib/tls/ca/"
	tlsCACertName      = "ca.crt"
	tlsSecretMountPath = "/var/lib/tls/secret/" //nolint
	tlsSecretCertName  = "tls.crt"              //nolint
	tlsSecretKeyName   = "tls.key"
	tlsServerMountPath = "/var/lib/tls/server/"
	tlsServerFileName  = "server.pem"

	// lastVersionAnnotationKey should indicate which version of MongoDB was last
	// configured
	lastVersionAnnotationKey = "mongodb.com/v1.lastVersion"
	// TLSRolledOutKey indicates if TLS has been fully rolled out
	tLSRolledOutAnnotationKey      = "mongodb.com/v1.tlsRolledOut"
	hasLeftReadyStateAnnotationKey = "mongodb.com/v1.hasLeftReadyStateAnnotationKey"

	trueAnnotation = "true"
)

// Add creates a new MongoDB Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr, readVersionManifestFromDisk))
}

// ManifestProvider is a function which returns the VersionManifest which
// contains the list of all available MongoDB versions
type ManifestProvider func() (automationconfig.VersionManifest, error)

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, manifestProvider ManifestProvider) reconcile.Reconciler {
	mgrClient := mgr.GetClient()
	return &ReplicaSetReconciler{
		client:           mdbClient.NewClient(mgrClient),
		scheme:           mgr.GetScheme(),
		manifestProvider: manifestProvider,
		log:              zap.S(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("replicaset-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MongoDB
	err = c.Watch(&source.Kind{Type: &mdbv1.MongoDB{}}, &handler.EnqueueRequestForObject{}, predicates.OnlyOnSpecChange())
	if err != nil {
		return err
	}
	return nil
}

// blank assignment to verify that ReplicaSetReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReplicaSetReconciler{}

// ReplicaSetReconciler reconciles a MongoDB ReplicaSet
type ReplicaSetReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client           mdbClient.Client
	scheme           *runtime.Scheme
	manifestProvider func() (automationconfig.VersionManifest, error)
	log              *zap.SugaredLogger
}

// Reconcile reads that state of the cluster for a MongoDB object and makes changes based on the state read
// and what is in the MongoDB.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReplicaSetReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	r.log = zap.S().With("ReplicaSet", request.NamespacedName)
	r.log.Info("Reconciling MongoDB")

	// TODO: generalize preparation for resource
	// Fetch the MongoDB instance
	mdb := mdbv1.MongoDB{}
	err := r.client.Get(context.TODO(), request.NamespacedName, &mdb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		r.log.Errorf("error reconciling MongoDB resource: %s", err)
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if err := r.ensureAutomationConfig(mdb); err != nil {
		r.log.Warnf("error creating automation config config map: %s", err)
		return reconcile.Result{}, err
	}

	r.log.Debug("Ensuring the service exists")
	if err := r.ensureService(mdb); err != nil {
		r.log.Warnf("Error ensuring the service exists: %s", err)
		return reconcile.Result{}, err
	}

	isTLSValid, err := r.validateTLSConfig(mdb)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !isTLSValid {
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	r.log.Debug("Creating/Updating StatefulSet")
	if err := r.createOrUpdateStatefulSet(mdb); err != nil {
		r.log.Warnf("Error creating/updating StatefulSet: %+v", err)
		return reconcile.Result{}, err
	}

	currentSts := appsv1.StatefulSet{}
	if err := r.client.Get(context.TODO(), mdb.NamespacedName(), &currentSts); err != nil {
		r.log.Warnf("Error getting StatefulSet: %s", err)
		return reconcile.Result{}, err
	}

	r.log.Debugf("Ensuring StatefulSet is ready, with type: %s", getUpdateStrategyType(mdb))
	ready, err := r.isStatefulSetReady(mdb, &currentSts)
	if err != nil {
		r.log.Warnf("error checking StatefulSet status: %+v", err)
		return reconcile.Result{}, err
	}

	if !ready {
		r.log.Infof("StatefulSet %s/%s is not yet ready, retrying in 10 seconds", mdb.Namespace, mdb.Name)
		return reconcile.Result{RequeueAfter: time.Second * 10}, nil
	}

	r.log.Debug("Resetting StatefulSet UpdateStrategy")
	if err := r.resetStatefulSetUpdateStrategy(mdb); err != nil {
		r.log.Warnf("error resetting StatefulSet UpdateStrategyType: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Debug("Setting MongoDB Annotations")

	annotations := map[string]string{
		lastVersionAnnotationKey:       mdb.Spec.Version,
		hasLeftReadyStateAnnotationKey: "false",
	}
	if err := r.setAnnotations(mdb.NamespacedName(), annotations); err != nil {
		r.log.Warnf("Error setting annotations: %+v", err)
		return reconcile.Result{}, err
	}

	if err := r.completeTLSRollout(mdb); err != nil {
		r.log.Warnf("Error completing TLS rollout: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Debug("Updating MongoDB Status")
	newStatus, err := r.updateAndReturnStatusSuccess(&mdb)
	if err != nil {
		r.log.Warnf("Error updating the status of the MongoDB resource: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Infow("Successfully finished reconciliation", "MongoDB.Spec:", mdb.Spec, "MongoDB.Status", newStatus)
	return reconcile.Result{}, nil
}

// resetStatefulSetUpdateStrategy ensures the stateful set is configured back to using RollingUpdateStatefulSetStrategyType
// and does not keep using OnDelete
func (r *ReplicaSetReconciler) resetStatefulSetUpdateStrategy(mdb mdbv1.MongoDB) error {
	if !isChangingVersion(mdb) {
		return nil
	}
	// if we changed the version, we need to reset the UpdatePolicy back to OnUpdate
	return statefulset.GetAndUpdate(r.client, mdb.NamespacedName(), func(sts *appsv1.StatefulSet) {
		sts.Spec.UpdateStrategy.Type = appsv1.RollingUpdateStatefulSetStrategyType
	})
}

// isStatefulSetReady checks to see if the stateful set corresponding to the given MongoDB resource
// is currently ready.
func (r *ReplicaSetReconciler) isStatefulSetReady(mdb mdbv1.MongoDB, existingStatefulSet *appsv1.StatefulSet) (bool, error) {
	stsFunc := buildStatefulSetModificationFunction(mdb)
	stsCopy := existingStatefulSet.DeepCopyObject()
	stsFunc(existingStatefulSet)
	stsCopyBytes, err := json.Marshal(stsCopy)
	if err != nil {
		return false, err
	}

	stsBytes, err := json.Marshal(existingStatefulSet)
	if err != nil {
		return false, err
	}

	//comparison is done with bytes instead of reflect.DeepEqual as there are
	//some issues with nil/empty maps not being compared correctly otherwise
	areEqual := bytes.Equal(stsCopyBytes, stsBytes)

	isReady := statefulset.IsReady(*existingStatefulSet, mdb.Spec.Members)
	if existingStatefulSet.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType && !isReady {
		r.log.Info("StatefulSet has left ready state, version upgrade in progress")
		annotations := map[string]string{
			hasLeftReadyStateAnnotationKey: trueAnnotation,
		}
		if err := r.setAnnotations(mdb.NamespacedName(), annotations); err != nil {
			return false, fmt.Errorf("failed setting %s annotation to true: %s", hasLeftReadyStateAnnotationKey, err)
		}
	}

	hasPerformedUpgrade := mdb.Annotations[hasLeftReadyStateAnnotationKey] == trueAnnotation
	r.log.Infow("StatefulSet Readiness", "isReady", isReady, "hasPerformedUpgrade", hasPerformedUpgrade, "areEqual", areEqual)

	if existingStatefulSet.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		return areEqual && isReady && hasPerformedUpgrade, nil
	}

	return areEqual && isReady, nil
}

func (r *ReplicaSetReconciler) ensureService(mdb mdbv1.MongoDB) error {
	svc := buildService(mdb)
	err := r.client.Create(context.TODO(), &svc)
	if err != nil && errors.IsAlreadyExists(err) {
		r.log.Infof("The service already exists... moving forward: %s", err)
		return nil
	}
	return err
}

func (r *ReplicaSetReconciler) createOrUpdateStatefulSet(mdb mdbv1.MongoDB) error {
	set := appsv1.StatefulSet{}
	err := r.client.Get(context.TODO(), mdb.NamespacedName(), &set)
	err = k8sClient.IgnoreNotFound(err)
	if err != nil {
		return fmt.Errorf("error getting StatefulSet: %s", err)
	}
	buildStatefulSetModificationFunction(mdb)(&set)
	if err = statefulset.CreateOrUpdate(r.client, set); err != nil {
		return fmt.Errorf("error creating/updating StatefulSet: %s", err)
	}
	return nil
}

// setAnnotations updates the monogdb resource annotations by applying the provided annotations
// on top of the existing ones
func (r ReplicaSetReconciler) setAnnotations(nsName types.NamespacedName, annotations map[string]string) error {
	mdb := mdbv1.MongoDB{}
	return r.client.GetAndUpdate(nsName, &mdb, func() {
		if mdb.Annotations == nil {
			mdb.Annotations = map[string]string{}
		}
		for key, val := range annotations {
			mdb.Annotations[key] = val
		}
	})
}

// updateAndReturnStatusSuccess should be called after a successful reconciliation
// the resource's status is updated to reflect to the state, and any other cleanup
// operators should be performed here
func (r ReplicaSetReconciler) updateAndReturnStatusSuccess(mdb *mdbv1.MongoDB) (mdbv1.MongoDBStatus, error) {
	newMdb := &mdbv1.MongoDB{}
	if err := r.client.Get(context.TODO(), mdb.NamespacedName(), newMdb); err != nil {
		return mdbv1.MongoDBStatus{}, fmt.Errorf("error getting resource: %+v", err)
	}
	newMdb.UpdateSuccess()
	if err := r.client.Status().Update(context.TODO(), newMdb); err != nil {
		return mdbv1.MongoDBStatus{}, fmt.Errorf("error updating status: %+v", err)
	}
	return newMdb.Status, nil
}

func (r ReplicaSetReconciler) ensureAutomationConfig(mdb mdbv1.MongoDB) error {
	cm, err := r.buildAutomationConfigConfigMap(mdb)
	if err != nil {
		return err
	}
	return configmap.CreateOrUpdate(r.client, cm)
}

func buildAutomationConfig(mdb mdbv1.MongoDB, mdbVersionConfig automationconfig.MongoDbVersionConfig, currentAc automationconfig.AutomationConfig, enabler automationconfig.AuthEnabler) (automationconfig.AutomationConfig, error) {
	domain := getDomain(mdb.ServiceName(), mdb.Namespace, "")

	builder := automationconfig.NewBuilder().
		SetTopology(automationconfig.ReplicaSetTopology).
		SetName(mdb.Name).
		SetDomain(domain).
		SetMembers(mdb.Spec.Members).
		SetPreviousAutomationConfig(currentAc).
		SetMongoDBVersion(mdb.Spec.Version).
		SetFCV(mdb.GetFCV()).
		AddVersion(mdbVersionConfig).
		SetAuthEnabler(enabler).
		SetToolsVersion(dummyToolsVersionConfig())

	// Enable TLS in the automation config after the certs and keys have been rolled out to all pods.
	// The agent needs these to be in place before the config is updated.
	// The agents will handle the gradual enabling of TLS as recommended in: https://docs.mongodb.com/manual/tutorial/upgrade-cluster-to-ssl/
	if mdb.Spec.Security.TLS.Enabled && hasRolledOutTLS(mdb) {
		mode := automationconfig.TLSModeRequired
		if mdb.Spec.Security.TLS.Optional {
			// TLSModePreferred requires server-server connections to use TLS but makes it optional for clients.
			mode = automationconfig.TLSModePreferred
		}

		builder.SetTLS(
			tlsCAMountPath+tlsCACertName,
			tlsServerMountPath+tlsServerFileName,
			mode,
		)
	}

	newAc, err := builder.Build()
	if err != nil {
		return automationconfig.AutomationConfig{}, err
	}

	return newAc, nil
}

// dummyToolsVersionConfig generates a dummy config for the tools settings in the automation config.
// The agent will not uses any of these values but requires them to be set.
// TODO: Remove this once the agent doesn't require any config: https://jira.mongodb.org/browse/CLOUDP-66024.
func dummyToolsVersionConfig() automationconfig.ToolsVersion {
	return automationconfig.ToolsVersion{
		Version: "100.0.2",
		URLs: map[string]map[string]string{
			// The OS must be correctly set. Our Docker image uses Ubuntu 16.04.
			"linux": {
				"ubuntu1604": "https://dummy",
			},
		},
	}
}

func readVersionManifestFromDisk() (automationconfig.VersionManifest, error) {
	bytes, err := ioutil.ReadFile(versionManifestFilePath)
	if err != nil {
		return automationconfig.VersionManifest{}, err
	}
	return versionManifestFromBytes(bytes)
}

func versionManifestFromBytes(bytes []byte) (automationconfig.VersionManifest, error) {
	versionManifest := automationconfig.VersionManifest{}
	if err := json.Unmarshal(bytes, &versionManifest); err != nil {
		return automationconfig.VersionManifest{}, err
	}
	return versionManifest, nil
}

// buildService creates a Service that will be used for the Replica Set StatefulSet
// that allows all the members of the STS to see each other.
// TODO: Make sure this Service is as minimal as posible, to not interfere with
// future implementations and Service Discovery mechanisms we might implement.
func buildService(mdb mdbv1.MongoDB) corev1.Service {
	label := make(map[string]string)
	label["app"] = mdb.ServiceName()
	return service.Builder().
		SetName(mdb.ServiceName()).
		SetNamespace(mdb.Namespace).
		SetSelector(label).
		SetServiceType(corev1.ServiceTypeClusterIP).
		SetClusterIP("None").
		SetPort(27017).
		Build()
}

func getCurrentAutomationConfig(getUpdater configmap.GetUpdater, mdb mdbv1.MongoDB) (automationconfig.AutomationConfig, error) {
	currentCm, err := getUpdater.GetConfigMap(types.NamespacedName{Name: mdb.ConfigMapName(), Namespace: mdb.Namespace})
	if err != nil {
		// If the AC was not found we don't surface it as an error
		return automationconfig.AutomationConfig{}, k8sClient.IgnoreNotFound(err)
	}

	currentAc := automationconfig.AutomationConfig{}
	if err := json.Unmarshal([]byte(currentCm.Data[AutomationConfigKey]), &currentAc); err != nil {
		return automationconfig.AutomationConfig{}, err
	}
	return currentAc, nil
}

func (r ReplicaSetReconciler) buildAutomationConfigConfigMap(mdb mdbv1.MongoDB) (corev1.ConfigMap, error) {

	manifest, err := r.manifestProvider()
	if err != nil {
		return corev1.ConfigMap{}, fmt.Errorf("error reading version manifest from disk: %+v", err)
	}

	enabler, err := getAuthenticationEnabler(r.client, mdb)
	if err != nil {
		return corev1.ConfigMap{}, err
	}

	currentAC, err := getCurrentAutomationConfig(r.client, mdb)
	if err != nil {
		return corev1.ConfigMap{}, err
	}

	ac, err := buildAutomationConfig(mdb, manifest.BuildsForVersion(mdb.Spec.Version), currentAC, enabler)
	if err != nil {
		return corev1.ConfigMap{}, err
	}
	acBytes, err := json.Marshal(ac)
	if err != nil {
		return corev1.ConfigMap{}, err
	}

	return configmap.Builder().
		SetName(mdb.ConfigMapName()).
		SetNamespace(mdb.Namespace).
		SetField(AutomationConfigKey, string(acBytes)).
		Build(), nil
}

// getUpdateStrategyType returns the type of RollingUpgradeStrategy that the StatefulSet
// should be configured with
func getUpdateStrategyType(mdb mdbv1.MongoDB) appsv1.StatefulSetUpdateStrategyType {
	if !isChangingVersion(mdb) {
		return appsv1.RollingUpdateStatefulSetStrategyType
	}
	return appsv1.OnDeleteStatefulSetStrategyType
}

// buildStatefulSet takes a MongoDB resource and converts it into
// the corresponding stateful set
func buildStatefulSet(mdb mdbv1.MongoDB) (appsv1.StatefulSet, error) {
	sts := appsv1.StatefulSet{}
	buildStatefulSetModificationFunction(mdb)(&sts)
	return sts, nil
}

func isChangingVersion(mdb mdbv1.MongoDB) bool {
	if lastVersion, ok := mdb.Annotations[lastVersionAnnotationKey]; ok {
		return (mdb.Spec.Version != lastVersion) && lastVersion != ""
	}
	return false
}

func mongodbAgentContainer(volumeMounts []corev1.VolumeMount) container.Modification {
	return container.Apply(
		container.WithName(agentName),
		container.WithImage(os.Getenv(agentImageEnv)),
		container.WithImagePullPolicy(corev1.PullAlways),
		container.WithReadinessProbe(defaultReadiness()),
		container.WithResourceRequirements(resourcerequirements.Defaults()),
		container.WithVolumeMounts(volumeMounts),
		container.WithCommand([]string{
			"agent/mongodb-agent",
			"-cluster=" + clusterFilePath,
			"-skipMongoStart",
			"-noDaemonize",
			"-healthCheckFilePath=" + agentHealthStatusFilePathValue,
			"-serveStatusPort=5000",
		},
		),
		container.WithEnvs(
			corev1.EnvVar{
				Name:  agentHealthStatusFilePathEnv,
				Value: agentHealthStatusFilePathValue,
			},
		),
	)
}

func versionUpgradeHookInit(volumeMount []corev1.VolumeMount) container.Modification {
	return container.Apply(
		container.WithName(versionUpgradeHookName),
		container.WithCommand([]string{"cp", "version-upgrade-hook", "/hooks/version-upgrade"}),
		container.WithImage(os.Getenv(versionUpgradeHookImageEnv)),
		container.WithImagePullPolicy(corev1.PullAlways),
		container.WithVolumeMounts(volumeMount),
	)
}

func mongodbContainer(version string, volumeMounts []corev1.VolumeMount) container.Modification {
	mongoDbCommand := []string{
		"/bin/sh",
		"-c",
		`
# run post-start hook to handle version changes
/hooks/version-upgrade

# wait for config to be created by the agent
while [ ! -f /data/automation-mongod.conf ]; do sleep 3 ; done ; sleep 2 ;

# start mongod with this configuration
exec mongod -f /data/automation-mongod.conf ;
`,
	}

	return container.Apply(
		container.WithName(mongodbName),
		container.WithImage(fmt.Sprintf("mongo:%s", version)),
		container.WithResourceRequirements(resourcerequirements.Defaults()),
		container.WithCommand(mongoDbCommand),
		container.WithEnvs(
			corev1.EnvVar{
				Name:  agentHealthStatusFilePathEnv,
				Value: "/healthstatus/agent-health-status.json",
			},
		),
		container.WithVolumeMounts(volumeMounts),
	)
}

func buildStatefulSetModificationFunction(mdb mdbv1.MongoDB) statefulset.Modification {
	labels := map[string]string{
		"app": mdb.ServiceName(),
	}

	ownerReferences := []metav1.OwnerReference{
		*metav1.NewControllerRef(&mdb, schema.GroupVersionKind{
			Group:   mdbv1.SchemeGroupVersion.Group,
			Version: mdbv1.SchemeGroupVersion.Version,
			Kind:    mdb.Kind,
		}),
	}

	// the health status volume is required in both agent and mongod pods.
	// the mongod requires it to determine if an upgrade is happening and needs to kill the pod
	// to prevent agent deadlock
	healthStatusVolume := statefulset.CreateVolumeFromEmptyDir("healthstatus")
	agentHealthStatusVolumeMount := statefulset.CreateVolumeMount(healthStatusVolume.Name, "/var/log/mongodb-mms-automation/healthstatus")
	mongodHealthStatusVolumeMount := statefulset.CreateVolumeMount(healthStatusVolume.Name, "/healthstatus")

	// hooks volume is only required on the mongod pod.
	hooksVolume := statefulset.CreateVolumeFromEmptyDir("hooks")
	hooksVolumeMount := statefulset.CreateVolumeMount(hooksVolume.Name, "/hooks", statefulset.WithReadOnly(false))

	automationConfigVolume := statefulset.CreateVolumeFromConfigMap("automation-config", mdb.ConfigMapName())
	automationConfigVolumeMount := statefulset.CreateVolumeMount(automationConfigVolume.Name, "/var/lib/automation/config", statefulset.WithReadOnly(true))

	dataVolume := statefulset.CreateVolumeMount(dataVolumeName, "/data")

	return statefulset.Apply(
		statefulset.WithName(mdb.Name),
		statefulset.WithNamespace(mdb.Namespace),
		statefulset.WithServiceName(mdb.ServiceName()),
		statefulset.WithLabels(labels),
		statefulset.WithMatchLabels(labels),
		statefulset.WithOwnerReference(ownerReferences),
		statefulset.WithReplicas(mdb.Spec.Members),
		statefulset.WithUpdateStrategyType(getUpdateStrategyType(mdb)),
		statefulset.WithVolumeClaim(dataVolumeName, defaultPvc()),
		statefulset.WithPodSpecTemplate(
			podtemplatespec.Apply(
				podtemplatespec.WithPodLabels(labels),
				podtemplatespec.WithVolume(healthStatusVolume),
				podtemplatespec.WithVolume(hooksVolume),
				podtemplatespec.WithVolume(automationConfigVolume),
				podtemplatespec.WithServiceAccount(operatorServiceAccountName),
				podtemplatespec.WithContainer(agentName, mongodbAgentContainer([]corev1.VolumeMount{agentHealthStatusVolumeMount, automationConfigVolumeMount, dataVolume})),
				podtemplatespec.WithContainer(mongodbName, mongodbContainer(mdb.Spec.Version, []corev1.VolumeMount{mongodHealthStatusVolumeMount, dataVolume, hooksVolumeMount})),
				podtemplatespec.WithInitContainer(versionUpgradeHookName, versionUpgradeHookInit([]corev1.VolumeMount{hooksVolumeMount})),
				buildTLSPodSpecModification(mdb),
				buildScramPodSpecModification(mdb),
			),
		),
	)
}

func getDomain(service, namespace, clusterName string) string {
	if clusterName == "" {
		clusterName = "cluster.local"
	}
	return fmt.Sprintf("%s.%s.svc.%s", service, namespace, clusterName)
}

func defaultReadiness() probes.Modification {
	return probes.Apply(
		probes.WithExecCommand([]string{readinessProbePath}),
		probes.WithFailureThreshold(240),
		probes.WithInitialDelaySeconds(5),
	)
}

func defaultPvc() persistentvolumeclaim.Modification {
	return persistentvolumeclaim.Apply(
		persistentvolumeclaim.WithName(dataVolumeName),
		persistentvolumeclaim.WithAccessModes(corev1.ReadWriteOnce),
		persistentvolumeclaim.WithResourceRequests(resourcerequirements.BuildDefaultStorageRequirements()),
	)
}
