package mongodb

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/secret"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/configmap"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/podtemplatespec"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/statefulset"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/pkg/apis/mongodb/v1"
)

const (
	tlsCAMountPath             = "/var/lib/tls/ca/"
	tlsCACertName              = "ca.crt"
	tlsOperatorSecretMountPath = "/var/lib/tls/server/" //nolint
	tlsSecretCertName          = "tls.crt"              //nolint
	tlsSecretKeyName           = "tls.key"
)

// validateTLSConfig will check that the configured ConfigMap and Secret exist and that they have the correct fields.
func (r *ReplicaSetReconciler) validateTLSConfig(mdb mdbv1.MongoDB) (bool, error) {
	if !mdb.Spec.Security.TLS.Enabled {
		return true, nil
	}

	r.log.Info("Ensuring TLS is correctly configured")

	// Ensure CA ConfigMap exists
	caData, err := configmap.ReadData(r.client, mdb.TLSConfigMapNamespacedName())
	if err != nil {
		if apiErrors.IsNotFound(err) {
			r.log.Warnf(`CA ConfigMap "%s" not found`, mdb.TLSConfigMapNamespacedName())
			return false, nil
		}

		return false, err
	}

	// Ensure ConfigMap has a "ca.crt" field
	if cert, ok := caData[tlsCACertName]; !ok || cert == "" {
		r.log.Warnf(`ConfigMap "%s" should have a CA certificate in field "%s"`, mdb.TLSConfigMapNamespacedName(), tlsCACertName)
		return false, nil
	}

	// Ensure Secret exists
	secretData, err := secret.ReadStringData(r.client, mdb.TLSSecretNamespacedName())
	if err != nil {
		if apiErrors.IsNotFound(err) {
			r.log.Warnf(`Secret "%s" not found`, mdb.TLSSecretNamespacedName())
			return false, nil
		}

		return false, err
	}

	// Ensure Secret has "tls.crt" and "tls.key" fields
	if key, ok := secretData[tlsSecretKeyName]; !ok || key == "" {
		r.log.Warnf(`Secret "%s" should have a key in field "%s"`, mdb.TLSSecretNamespacedName(), tlsSecretKeyName)
		return false, nil
	}
	if cert, ok := secretData[tlsSecretCertName]; !ok || cert == "" {
		r.log.Warnf(`Secret "%s" should have a certificate in field "%s"`, mdb.TLSSecretNamespacedName(), tlsSecretKeyName)
		return false, nil
	}

	// Watch certificate-key secret to handle rotations
	r.secretWatcher.Watch(mdb.TLSSecretNamespacedName(), mdb.NamespacedName())

	return true, nil
}

// getTLSConfigModification creates a modification function which enables TLS in the automation config.
// It will also ensure that the combined cert-key secret is created.
func getTLSConfigModification(getUpdateCreator secret.GetUpdateCreator, mdb mdbv1.MongoDB) (automationconfig.Modification, error) {
	if !mdb.Spec.Security.TLS.Enabled {
		return automationconfig.NOOP(), nil
	}

	certKey, err := getCertAndKey(getUpdateCreator, mdb)
	if err != nil {
		return automationconfig.NOOP(), err
	}

	err = ensureTLSSecret(getUpdateCreator, mdb, certKey)
	if err != nil {
		return automationconfig.NOOP(), err
	}

	// The config is only updated after the certs and keys have been rolled out to all pods.
	// The agent needs these to be in place before the config is updated.
	// Once the config is updated, the agents will gradually enable TLS in accordance with: https://docs.mongodb.com/manual/tutorial/upgrade-cluster-to-ssl/
	if hasRolledOutTLS(mdb) {
		return tlsConfigModification(mdb, certKey), nil
	}

	return automationconfig.NOOP(), nil
}

// getCertAndKey will fetch the certificate and key from the user-provided Secret.
func getCertAndKey(getter secret.Getter, mdb mdbv1.MongoDB) (string, error) {
	cert, err := secret.ReadKey(getter, tlsSecretCertName, mdb.TLSSecretNamespacedName())
	if err != nil {
		return "", err
	}

	key, err := secret.ReadKey(getter, tlsSecretKeyName, mdb.TLSSecretNamespacedName())
	if err != nil {
		return "", err
	}

	return combineCertificateAndKey(cert, key), nil
}

func combineCertificateAndKey(cert, key string) string {
	trimmedCert := strings.TrimRight(cert, "\n")
	trimmedKey := strings.TrimRight(key, "\n")
	return fmt.Sprintf("%s\n%s", trimmedCert, trimmedKey)
}

// ensureTLSSecret will create or update the operator-managed Secret containing
// the concatenated certificate and key from the user-provided Secret.
func ensureTLSSecret(getUpdateCreator secret.GetUpdateCreator, mdb mdbv1.MongoDB, certKey string) error {
	// Calculate file name from certificate and key
	fileName := tlsOperatorSecretFileName(certKey)

	operatorSecret := secret.Builder().
		SetName(mdb.TLSOperatorSecretNamespacedName().Name).
		SetNamespace(mdb.TLSOperatorSecretNamespacedName().Namespace).
		SetField(fileName, certKey).
		SetOwnerReferences([]metav1.OwnerReference{getOwnerReference(mdb)}).
		Build()

	return secret.CreateOrUpdate(getUpdateCreator, operatorSecret)
}

// tlsOperatorSecretFileName calculates the file name to use for the mounted
// certificate-key file. The name is based on the hash of the combined cert and key.
// If the certificate or key changes, the file path changes as well which will trigger
// the agent to perform a restart.
// The user-provided secret is being watched and will trigger a reconciliation
// on changes. This enables the operator to automatically handle cert rotations.
func tlsOperatorSecretFileName(certKey string) string {
	hash := sha256.Sum256([]byte(certKey))
	return fmt.Sprintf("%x.pem", hash)
}

// tlsConfigModification will enable TLS in the automation config.
func tlsConfigModification(mdb mdbv1.MongoDB, certKey string) automationconfig.Modification {
	caCertificatePath := tlsCAMountPath + tlsCACertName
	certificateKeyPath := tlsOperatorSecretMountPath + tlsOperatorSecretFileName(certKey)

	mode := automationconfig.TLSModeRequired
	if mdb.Spec.Security.TLS.Optional {
		// TLSModePreferred requires server-server connections to use TLS but makes it optional for clients.
		mode = automationconfig.TLSModePreferred
	}

	return func(config *automationconfig.AutomationConfig) {
		// Configure CA certificate for agent
		config.TLS.CAFilePath = caCertificatePath

		for i := range config.Processes {
			args := config.Processes[i].Args26

			args.Set("net.tls.mode", mode)
			args.Set("net.tls.CAFile", caCertificatePath)
			args.Set("net.tls.certificateKeyFile", certificateKeyPath)
			args.Set("net.tls.allowConnectionsWithoutCertificates", true)
		}
	}
}

// hasRolledOutTLS determines if the TLS key and certs have been mounted to all pods.
// These must be mounted before TLS can be enabled in the automation config.
func hasRolledOutTLS(mdb mdbv1.MongoDB) bool {
	_, completedRollout := mdb.Annotations[tlsRolledOutAnnotationKey]
	return completedRollout
}

// completeTLSRollout will update the automation config and set an annotation indicating that TLS has been rolled out.
// At this stage, TLS hasn't yet been enabled but the keys and certs have all been mounted.
// The automation config will be updated and the agents will continue work on gradually enabling TLS across the replica set.
func (r *ReplicaSetReconciler) completeTLSRollout(mdb mdbv1.MongoDB) error {
	if !mdb.Spec.Security.TLS.Enabled || hasRolledOutTLS(mdb) {
		return nil
	}

	r.log.Debug("Completing TLS rollout")

	mdb.Annotations[tlsRolledOutAnnotationKey] = trueAnnotation
	if err := r.ensureAutomationConfig(mdb); err != nil {
		return errors.Errorf("could not update automation config after TLS rollout: %s", err)
	}

	if err := r.setAnnotations(mdb.NamespacedName(), mdb.Annotations); err != nil {
		return errors.Errorf("could not set TLS annotation: %s", err)
	}

	return nil
}

// buildTLSPodSpecModification will add the TLS init container and volumes to the pod template if TLS is enabled.
func buildTLSPodSpecModification(mdb mdbv1.MongoDB) podtemplatespec.Modification {
	if !mdb.Spec.Security.TLS.Enabled {
		return podtemplatespec.NOOP()
	}

	// Configure a volume which mounts the CA certificate from a ConfigMap
	// The certificate is used by both mongod and the agent
	caVolume := statefulset.CreateVolumeFromConfigMap("tls-ca", mdb.Spec.Security.TLS.CaConfigMap.Name)
	caVolumeMount := statefulset.CreateVolumeMount(caVolume.Name, tlsCAMountPath, statefulset.WithReadOnly(true))

	// Configure a volume which mounts the secret holding the server key and certificate
	// The same key-certificate pair is used for all servers
	tlsSecretVolume := statefulset.CreateVolumeFromSecret("tls-secret", mdb.TLSOperatorSecretNamespacedName().Name)
	tlsSecretVolumeMount := statefulset.CreateVolumeMount(tlsSecretVolume.Name, tlsOperatorSecretMountPath, statefulset.WithReadOnly(true))

	// MongoDB expects both key and certificate to be provided in a single PEM file
	// We are using a secret format where they are stored in separate fields, tls.crt and tls.key
	// Because of this we need to use an init container which reads the two files mounted from the secret and combines them into one
	return podtemplatespec.Apply(
		podtemplatespec.WithVolume(caVolume),
		podtemplatespec.WithVolume(tlsSecretVolume),
		podtemplatespec.WithVolumeMounts(agentName, tlsSecretVolumeMount, caVolumeMount),
		podtemplatespec.WithVolumeMounts(mongodbName, tlsSecretVolumeMount, caVolumeMount),
	)
}
