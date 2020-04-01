package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	AutomationConfigKey        = "automation-config"
	agentName                  = "mongodb-agent"
	mongodbName                = "mongod"
	agentImageEnvVariable      = "AGENT_IMAGE"
	versionManifestFilePath    = "/usr/local/version_manifest.json"
	readinessProbePath         = "/var/lib/mongodb-mms-automation/probes/readinessprobe"
	agentHealthStatusFilePath  = "/var/log/mongodb-mms-automation/healthstatus/agent-health-status.json"
	clusterFilePath            = "/var/lib/automation/config/automation-config"
	operatorServiceAccountName = "mongodb-kubernetes-operator"
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

	// TODO: Read current automation config version from config map
	if err := r.ensureAutomationConfig(mdb); err != nil {
		r.log.Infof("Error creating automation config config map: %s", err)
		return reconcile.Result{}, err
	}

	svc := buildService(mdb)
	if err = r.client.CreateOrUpdate(&svc); err != nil {
		r.log.Infof("The service already exists... moving forward: %s", err)
	}

	if err := r.createOrUpdateStatefulSet(mdb); err != nil {
		r.log.Infof("Error creating/updating StatefulSet: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Debugf("Sleeping for 2 seconds...")
	time.Sleep(time.Second * 2)

	r.log.Debug("Waiting for StatefulSet to be ready.")
	if ready, err := r.isStatefulSetReady(mdb); err != nil {
		r.log.Infof("Error checking StatefulSet status: %+v", err)
		return reconcile.Result{}, err
	} else if !ready {
		r.log.Infof("StatefulSet %s/%s is not yet ready, retrying in 10 seconds", mdb.Namespace, mdb.Name)
		return reconcile.Result{RequeueAfter: time.Second * 10}, nil
	}

	r.log.Debug("Configuring StatefulSet UpdateStrategy.")
	if err := r.resetStatefulSetUpdateStrategy(mdb); err != nil {
		r.log.Infof("Error resetting StatefulSet UpdateStrategyType: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Debug("Updating MongoDB success status.")
	if err := r.updateStatusSuccess(&mdb); err != nil {
		r.log.Infof("Error updating the status of the MongoDB resource: %+v", err)
		return reconcile.Result{}, err
	}

	r.log.Info("Successfully finished reconciliation", "MongoDB.Spec:", mdb.Spec, "MongoDB.Status", mdb.Status)
	return reconcile.Result{}, nil
}

// resetStatefulSetUpdateStrategy ensures the stateful set is configured back to using RollingUpdateStatefulSetStrategyType
// and does not keep using OnDelete
func (r *ReplicaSetReconciler) resetStatefulSetUpdateStrategy(mdb mdbv1.MongoDB) error {
	// if we changed the version, we need to reset the UpdatePolicy back to OnUpdate
	sts := &appsv1.StatefulSet{}
	return r.client.GetAndUpdate(types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, sts, func() {
		sts.Spec.UpdateStrategy.Type = appsv1.RollingUpdateStatefulSetStrategyType
	})
}

// isStatefulSetReady checks to see if the stateful set corresponding to the given MongoDB resource
// is currently in the ready state
func (r *ReplicaSetReconciler) isStatefulSetReady(mdb mdbv1.MongoDB) (bool, error) {
	set := appsv1.StatefulSet{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, &set); err != nil {
		return false, fmt.Errorf("error getting StatefulSet: %s", err)
	}
	return statefulset.IsReady(set), nil
}

func (r *ReplicaSetReconciler) createOrUpdateStatefulSet(mdb mdbv1.MongoDB) error {
	sts, err := buildStatefulSet(mdb)
	if err != nil {
		return fmt.Errorf("error building StatefulSet: %s", err)
	}

	versionChangeInProgress := "false"
	if sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		versionChangeInProgress = "true"
	}

	r.log.Debugf("Configuring StatefulSet with the %s update strategy type", sts.Spec.UpdateStrategy.Type)
	if err := r.setAnnotation(types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, mdbv1.VersionChangeInProgress, versionChangeInProgress); err != nil {
		return fmt.Errorf("error updating MongoDB annotations")
	}

	if err = r.client.CreateOrUpdate(&sts); err != nil {
		return fmt.Errorf("error creating/updating StatefulSet: %s", err)
	}
	return nil
}

// setAnnotation merges the provided annotations with those of the given mongodb resource
func (r ReplicaSetReconciler) mergeAnnotations(nsName types.NamespacedName, annotations map[string]string) error {
	mdb := mdbv1.MongoDB{}
	return r.client.GetAndUpdate(nsName, &mdb, func() {
		if mdb.Annotations == nil {
			mdb.Annotations = map[string]string{}
		}
		for k, v := range annotations {
			mdb.Annotations[k] = v
		}
	})
}

// setAnnotation updates the annotation of a single MongoDB resource of the given nsName
func (r *ReplicaSetReconciler) setAnnotation(nsName types.NamespacedName, key, val string) error {
	return r.mergeAnnotations(nsName, map[string]string{
		key: val,
	})
}

// updateStatusSuccess should be called after a successful reconciliation
// the resource's status is updated to reflect to the state, and any other cleanup
// operators should be performed here
func (r ReplicaSetReconciler) updateStatusSuccess(mdb *mdbv1.MongoDB) error {
	newMdb := &mdbv1.MongoDB{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, newMdb); err != nil {
		return fmt.Errorf("error getting resource: %+v", err)
	}
	newMdb.UpdateSuccess()
	if err := r.client.Status().Update(context.TODO(), newMdb); err != nil {
		return fmt.Errorf("error updating status: %+v", err)
	}

	// upon a successful reconciliation, we are no longer in the process of changing version
	desiredAnnotations := map[string]string{
		mdbv1.ReachedVersionAnnotationKey: mdb.Spec.Version,
		mdbv1.VersionChangeInProgress:     "false",
	}

	r.log.Debugf("Updating MongoDB resource with the following annotations: %+v", desiredAnnotations)
	if err := r.mergeAnnotations(types.NamespacedName{Name: mdb.Name, Namespace: mdb.Namespace}, desiredAnnotations); err != nil {
		r.log.Infof("Error setting annotation: %+v", err)
		return fmt.Errorf("error updating annotations: %+v", err)
	}

	return nil
}

func (r ReplicaSetReconciler) ensureAutomationConfig(mdb mdbv1.MongoDB) error {
	cm, err := r.buildAutomationConfigConfigMap(mdb)
	if err != nil {
		return err
	}
	if err := r.client.CreateOrUpdate(&cm); err != nil {
		return err
	}
	return nil
}

func buildAutomationConfig(mdb mdbv1.MongoDB, mdbVersionConfig automationconfig.MongoDbVersionConfig) automationconfig.AutomationConfig {
	domain := getDomain(mdb.ServiceName(), mdb.Namespace, "")
	return automationconfig.NewBuilder().
		SetTopology(automationconfig.ReplicaSetTopology).
		SetName(mdb.Name).
		SetDomain(domain).
		SetMembers(mdb.Spec.Members).
		SetMongoDBVersion(mdb.Spec.Version).
		SetAutomationConfigVersion(1). // TODO: Correctly set the version
		SetFCV(mdb.GetFCV()).
		AddVersion(mdbVersionConfig).
		Build()
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

func (r ReplicaSetReconciler) buildAutomationConfigConfigMap(mdb mdbv1.MongoDB) (corev1.ConfigMap, error) {
	manifest, err := r.manifestProvider()
	if err != nil {
		return corev1.ConfigMap{}, fmt.Errorf("error reading version manifest from disk: %+v", err)
	}
	ac := buildAutomationConfig(mdb, manifest.BuildsForVersion(mdb.Spec.Version))
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

// buildContainers constructs the mongodb-agent container as well as the
// mongod container.
func buildContainers(mdb mdbv1.MongoDB) []corev1.Container {
	agentCommand := []string{
		"agent/mongodb-agent",
		"-cluster=" + clusterFilePath,
		"-skipMongoStart",
		"-noDaemonize",
		"-healthCheckFilePath=" + agentHealthStatusFilePath,
		"-serveStatusPort=5000",
	}

	readinessProbe := defaultReadinessProbe()
	agentContainer := corev1.Container{
		Name:            agentName,
		Image:           os.Getenv(agentImageEnvVariable),
		ImagePullPolicy: corev1.PullAlways,
		Resources:       resourcerequirements.Defaults(),
		Command:         agentCommand,
		ReadinessProbe:  &readinessProbe,
	}

	mongoDbCommand := []string{
		"/bin/sh",
		"-c",
		// pre-stop hooks only get executed on termination of the pod, not a successful completion. We need to manually
		// call the hook after the mongod command.
		`while [ ! -f /data/automation-mongod.conf ]; do sleep 3 ; done ; sleep 2;  mongod -f /data/automation-mongod.conf; /hooks/pre-stop;`,
	}

	mongodbContainer := corev1.Container{
		Name:      mongodbName,
		Image:     fmt.Sprintf("mongo:%s", mdb.Spec.Version),
		Command:   mongoDbCommand,
		Resources: resourcerequirements.Defaults(),
	}
	return []corev1.Container{agentContainer, mongodbContainer}
}

func buildInitContainers(preHookImage string, volumeMount corev1.VolumeMount) []corev1.Container {
	return []corev1.Container{
		{
			Name:  "mongod-prehook",
			Image: preHookImage,
			Command: []string{
				"cp", "/pre-stop-hook", "/hooks/pre-stop",
			},
			VolumeMounts:    []corev1.VolumeMount{volumeMount},
			ImagePullPolicy: corev1.PullAlways,
		},
	}
}

func defaultReadinessProbe() corev1.Probe {
	return corev1.Probe{
		Handler: corev1.Handler{
			Exec: &corev1.ExecAction{Command: []string{readinessProbePath}},
		},
		// Setting the failure threshold to quite big value as the agent may spend some time to reach the goal
		FailureThreshold: 240,
		// The agent may be not on time to write the status file right after the container is created - we need to wait
		// for some time
		InitialDelaySeconds: 5,
	}
}

// getUpdateStrategyType returns the type of RollingUpgradeStrategy that the StatefulSet
// should be configured with
func getUpdateStrategyType(mdb mdbv1.MongoDB) appsv1.StatefulSetUpdateStrategyType {
	// if we have a reconciliation that has the spec updated and also already has the desired
	// version annotation, we look for this flag to indicate we should continue using the OnDelete
	// strategy type
	if val, _ := mdb.Annotations[mdbv1.VersionChangeInProgress]; val == "true" {
		return appsv1.OnDeleteStatefulSetStrategyType
	}

	if val, ok := mdb.Annotations[mdbv1.ReachedVersionAnnotationKey]; ok {
		if mdb.Spec.Version != val && val != "" {
			return appsv1.OnDeleteStatefulSetStrategyType
		}
	}
	return appsv1.RollingUpdateStatefulSetStrategyType
}

// buildStatefulSet takes a MongoDB resource and converts it into
// the corresponding stateful set
func buildStatefulSet(mdb mdbv1.MongoDB) (appsv1.StatefulSet, error) {
	labels := map[string]string{
		"app": mdb.ServiceName(),
	}

	hooksVolumeMount := statefulset.CreateVolumeMount("hooks", "/hooks", statefulset.WithReadOnly(false))

	preHookImage := os.Getenv("PRE_STOP_HOOK_IMAGE")

	podSpecTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: operatorServiceAccountName,
			Containers:         buildContainers(mdb),
			InitContainers:     buildInitContainers(preHookImage, hooksVolumeMount),
		},
	}

	builder := statefulset.NewBuilder().
		SetPodTemplateSpec(podSpecTemplate).
		SetNamespace(mdb.Namespace).
		SetName(mdb.Name).
		SetReplicas(mdb.Spec.Members).
		SetLabels(labels).
		SetMatchLabels(labels).
		SetServiceName(mdb.ServiceName()).
		SetUpdateStrategy(getUpdateStrategyType(mdb))

	// TODO: Add this section to architecture document.
	// The design of the multi-container and the different volumes mounted to them is as follows:
	// There will be two volumes mounted:
	// 1. "data-volume": Access to /data for both agent and mongod. Shared data is required because
	//    agent writes automation-mongod.conf file in it and reads certain lock files from there.
	// 2. "automation-config": This is /var/lib/automation/config that holds the automation config
	//    mounted from a ConfigMap. This is only required in the Agent container.
	dataVolume, dataVolumeClaim := buildDataVolumeClaim()
	builder.
		AddVolumeMount(mongodbName, dataVolume).
		AddVolumeMount(agentName, dataVolume).
		AddVolumeClaimTemplates(dataVolumeClaim)
	// the automation config is only mounted, as read only, on the agent container
	automationConfigVolume := statefulset.CreateVolumeFromConfigMap("automation-config", mdb.ConfigMapName())
	automationConfigVolumeMount := statefulset.CreateVolumeMount(automationConfigVolume.Name, "/var/lib/automation/config", statefulset.WithReadOnly(true))
	builder.
		AddVolume(automationConfigVolume).
		AddVolumeMount(agentName, automationConfigVolumeMount).
		AddVolumeMount(mongodbName, automationConfigVolumeMount)

	// share the agent-health-status.json file in both containers
	healthStatusVolume := statefulset.CreateVolumeFromEmptyDir("healthstatus")
	builder.AddVolume(healthStatusVolume).
		AddVolumeMount(agentName, statefulset.CreateVolumeMount(healthStatusVolume.Name, "/var/log/mongodb-mms-automation/healthstatus")).
		AddVolumeMount(mongodbName, statefulset.CreateVolumeMount(healthStatusVolume.Name, "/healthstatus"))

	// Configure an empty volume on the mongod container into which the initContainer will copy over the pre-stop hook
	hooksVolume := statefulset.CreateVolumeFromEmptyDir("hooks")
	builder.AddVolume(hooksVolume).
		AddVolumeMount(mongodbName, hooksVolumeMount)

	return builder.Build()
}

func buildDataVolumeClaim() (corev1.VolumeMount, []corev1.PersistentVolumeClaim) {
	dataVolume := statefulset.CreateVolumeMount("data-volume", "/data")
	dataVolumeClaim := []corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "data-volume",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: resourcerequirements.BuildDefaultStorageRequirements(),
			},
		},
	}}

	return dataVolume, dataVolumeClaim
}

func getDomain(service, namespace, clusterName string) string {
	if clusterName == "" {
		clusterName = "cluster.local"
	}
	return fmt.Sprintf("%s.%s.svc.%s", service, namespace, clusterName)
}
