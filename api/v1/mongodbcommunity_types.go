package v1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/authentication/scram"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/annotations"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/scale"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Type string

const (
	ReplicaSet Type = "ReplicaSet"
)

type Phase string

const (
	Running Phase = "Running"
	Failed  Phase = "Failed"
	Pending Phase = "Pending"
)

const (
	defaultPasswordKey = "password"
)

// MongoDBCommunitySpec defines the desired state of MongoDB
type MongoDBCommunitySpec struct {
	// Members is the number of members in the replica set
	// +optional
	Members int `json:"members"`
	// Type defines which type of MongoDB deployment the resource should create
	// +kubebuilder:validation:Enum=ReplicaSet
	Type Type `json:"type"`
	// Version defines which version of MongoDB will be used
	Version string `json:"version"`

	// FeatureCompatibilityVersion configures the feature compatibility version that will
	// be set for the deployment
	// +optional
	FeatureCompatibilityVersion string `json:"featureCompatibilityVersion,omitempty"`

	// ReplicaSetHorizons Add this parameter and values if you need your database
	// to be accessed outside of Kubernetes. This setting allows you to
	// provide different DNS settings within the Kubernetes cluster and
	// to the Kubernetes cluster. The Kubernetes Operator uses split horizon
	// DNS for replica set members. This feature allows communication both
	// within the Kubernetes cluster and from outside Kubernetes.
	// +optional
	ReplicaSetHorizons ReplicaSetHorizonConfiguration `json:"replicaSetHorizons,omitempty"`

	// Security configures security features, such as TLS, and authentication settings for a deployment
	// +required
	Security Security `json:"security"`

	// Users specifies the MongoDB users that should be configured in your deployment
	// +required
	Users []MongoDBUser `json:"users"`

	// +optional
	StatefulSetConfiguration StatefulSetConfiguration `json:"statefulSet,omitempty"`

	// AdditionalMongodConfig is additional configuration that can be passed to
	// each data-bearing mongod at runtime. Uses the same structure as the mongod
	// configuration file: https://docs.mongodb.com/manual/reference/configuration-options/
	// +kubebuilder:validation:Type=object
	// +optional
	// +nullable
	AdditionalMongodConfig MongodConfiguration `json:"additionalMongodConfig,omitempty"`
}

// ReplicaSetHorizonConfiguration holds the split horizon DNS settings for
// replica set members.
type ReplicaSetHorizonConfiguration []automationconfig.ReplicaSetHorizons

// CustomRole defines a custom MongoDB role.
type CustomRole struct {
	// The name of the role.
	Role string `json:"role"`
	// The database of the role.
	DB string `json:"db"`
	// The privileges to grant the role.
	Privileges []Privilege `json:"privileges"`
	// An array of roles from which this role inherits privileges.
	// +optional
	Roles []Role `json:"roles"`
	// The authentication restrictions the server enforces on the role.
	// +optional
	AuthenticationRestrictions []AuthenticationRestriction `json:"authenticationRestrictions,omitempty"`
}

// ConvertToAutomationConfigCustomRole converts between a custom role defined by the crd and a custom role
// that can be used in the automation config.
func (c CustomRole) ConvertToAutomationConfigCustomRole() automationconfig.CustomRole {
	ac := automationconfig.CustomRole{Role: c.Role, DB: c.DB, Roles: []automationconfig.Role{}}

	// Add privileges.
	for _, privilege := range c.Privileges {
		ac.Privileges = append(ac.Privileges, automationconfig.Privilege{
			Resource: automationconfig.Resource{
				DB:          privilege.Resource.DB,
				Collection:  privilege.Resource.Collection,
				AnyResource: privilege.Resource.AnyResource,
				Cluster:     privilege.Resource.Cluster,
			},
			Actions: privilege.Actions,
		})
	}

	// Add roles.
	for _, dbRole := range c.Roles {
		ac.Roles = append(ac.Roles, automationconfig.Role{
			Role:     dbRole.Name,
			Database: dbRole.DB,
		})
	}

	// Add authentication restrictions (if any).
	for _, restriction := range c.AuthenticationRestrictions {
		ac.AuthenticationRestrictions = append(ac.AuthenticationRestrictions,
			automationconfig.AuthenticationRestriction{
				ClientSource:  restriction.ClientSource,
				ServerAddress: restriction.ServerAddress,
			})
	}

	return ac
}

// ConvertCustomRolesToAutomationConfigCustomRole converts custom roles to custom roles
// that can be used in the automation config.
func ConvertCustomRolesToAutomationConfigCustomRole(roles []CustomRole) []automationconfig.CustomRole {
	acRoles := []automationconfig.CustomRole{}
	for _, role := range roles {
		acRoles = append(acRoles, role.ConvertToAutomationConfigCustomRole())
	}
	return acRoles
}

// Privilege defines the actions a role is allowed to perform on a given resource.
type Privilege struct {
	Resource Resource `json:"resource"`
	Actions  []string `json:"actions"`
}

// Resource specifies specifies the resources upon which a privilege permits actions.
// See https://docs.mongodb.com/manual/reference/resource-document for more.
type Resource struct {
	// +optional
	DB *string `json:"db,omitempty"`
	// +optional
	Collection *string `json:"collection,omitempty"`
	// +optional
	Cluster bool `json:"cluster,omitempty"`
	// +optional
	AnyResource bool `json:"anyResource,omitempty"`
}

// AuthenticationRestriction specifies a list of IP addresses and CIDR ranges users
// are allowed to connect to or from.
type AuthenticationRestriction struct {
	ClientSource  []string `json:"clientSource"`
	ServerAddress []string `json:"serverAddress"`
}

// StatefulSetConfiguration holds the optional custom StatefulSet
// that should be merged into the operator created one.
type StatefulSetConfiguration struct {
	SpecWrapper StatefulSetSpecWrapper `json:"spec"`
}

// StatefulSetSpecWrapper is a wrapper around StatefulSetSpec with a custom implementation
// of MarshalJSON and UnmarshalJSON which delegate to the underlying Spec to avoid CRD pollution.

type StatefulSetSpecWrapper struct {
	Spec appsv1.StatefulSetSpec `json:"-"`
}

// MarshalJSON defers JSON encoding to the wrapped map
func (m *StatefulSetSpecWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Spec)
}

// UnmarshalJSON will decode the data into the wrapped map
func (m *StatefulSetSpecWrapper) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &m.Spec)
}

func (m *StatefulSetSpecWrapper) DeepCopy() *StatefulSetSpecWrapper {
	return &StatefulSetSpecWrapper{
		Spec: m.Spec,
	}
}

// MongodConfiguration holds the optional mongod configuration
// that should be merged with the operator created one.
//
// The CRD generator does not support map[string]interface{}
// on the top level and hence we need to work around this with
// a wrapping struct.
type MongodConfiguration struct {
	Object map[string]interface{} `json:"-"`
}

// MarshalJSON defers JSON encoding to the wrapped map
func (m *MongodConfiguration) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Object)
}

// UnmarshalJSON will decode the data into the wrapped map
func (m *MongodConfiguration) UnmarshalJSON(data []byte) error {
	if m.Object == nil {
		m.Object = map[string]interface{}{}
	}

	return json.Unmarshal(data, &m.Object)
}

func (m *MongodConfiguration) DeepCopy() *MongodConfiguration {
	return &MongodConfiguration{
		Object: runtime.DeepCopyJSON(m.Object),
	}
}

type MongoDBUser struct {
	// Name is the username of the user
	Name string `json:"name"`

	// DB is the database the user is stored in. Defaults to "admin"
	// +optional
	DB string `json:"db"`

	// PasswordSecretRef is a reference to the secret containing this user's password
	PasswordSecretRef SecretKeyReference `json:"passwordSecretRef"`

	// Roles is an array of roles assigned to this user
	Roles []Role `json:"roles"`

	// ScramCredentialsSecretName appended by string "scram-credentials" is the name of the secret object created by the mongoDB operator for storing SCRAM credentials
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$
	ScramCredentialsSecretName string `json:"scramCredentialsSecretName"`
}

func (m MongoDBUser) GetPasswordSecretKey() string {
	if m.PasswordSecretRef.Key == "" {
		return defaultPasswordKey
	}
	return m.PasswordSecretRef.Key
}

// GetScramCredentialsSecretName gets the final SCRAM credentials secret-name by appending the user provided
// scramsCredentialSecretName with "scram-credentials"
func (m MongoDBUser) GetScramCredentialsSecretName() string {
	return fmt.Sprintf("%s-%s", m.ScramCredentialsSecretName, "scram-credentials")
}

// SecretKeyReference is a reference to the secret containing the user's password
type SecretKeyReference struct {
	// Name is the name of the secret storing this user's password
	Name string `json:"name"`

	// Key is the key in the secret storing this password. Defaults to "password"
	// +optional
	Key string `json:"key"`
}

// Role is the database role this user should have
type Role struct {
	// DB is the database the role can act on
	DB string `json:"db"`
	// Name is the name of the role
	Name string `json:"name"`
}

type Security struct {
	// +optional
	Authentication Authentication `json:"authentication"`
	// TLS configuration for both client-server and server-server communication
	// +optional
	TLS TLS `json:"tls"`
	// User-specified custom MongoDB roles that should be configured in the deployment.
	// +optional
	Roles []CustomRole `json:"roles,omitempty"`
}

// TLS is the configuration used to set up TLS encryption
type TLS struct {
	Enabled bool `json:"enabled"`

	// Optional configures if TLS should be required or optional for connections
	// +optional
	Optional bool `json:"optional"`

	// CertificateKeySecret is a reference to a Secret containing a private key and certificate to use for TLS.
	// The key and cert are expected to be PEM encoded and available at "tls.key" and "tls.crt".
	// This is the same format used for the standard "kubernetes.io/tls" Secret type, but no specific type is required.
	// +optional
	CertificateKeySecret LocalObjectReference `json:"certificateKeySecretRef"`

	// CaConfigMap is a reference to a ConfigMap containing the certificate for the CA which signed the server certificates
	// The certificate is expected to be available under the key "ca.crt"
	// +optional
	CaConfigMap LocalObjectReference `json:"caConfigMapRef"`
}

// LocalObjectReference is a reference to another Kubernetes object by name.
// TODO: Replace with a type from the K8s API. CoreV1 has an equivalent
// 	"LocalObjectReference" type but it contains a TODO in its
// 	description that we don't want in our CRD.
type LocalObjectReference struct {
	Name string `json:"name"`
}

type Authentication struct {
	// Modes is an array specifying which authentication methods should be enabled.
	Modes []AuthMode `json:"modes"`

	// IgnoreUnknownUsers set to true will ensure any users added manually (not through the CRD)
	// will not be removed.

	// TODO: defaults will work once we update to v1 CRD.

	// +optional
	// +kubebuilder:default:=true
	// +nullable
	IgnoreUnknownUsers *bool `json:"ignoreUnknownUsers,omitempty"`
}

// +kubebuilder:validation:Enum=SCRAM
type AuthMode string

// MongoDBCommunityStatus defines the observed state of MongoDB
type MongoDBCommunityStatus struct {
	MongoURI string `json:"mongoUri"`
	Phase    Phase  `json:"phase"`

	CurrentStatefulSetReplicas int `json:"currentStatefulSetReplicas"`
	CurrentMongoDBMembers      int `json:"currentMongoDBMembers"`

	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MongoDBCommunity is the Schema for the mongodbs API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=mongodbcommunity,scope=Namespaced,shortName=mdbc,singular=mongodbcommunity
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current state of the MongoDB deployment"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.version",description="Version of MongoDB server"
type MongoDBCommunity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MongoDBCommunitySpec   `json:"spec,omitempty"`
	Status MongoDBCommunityStatus `json:"status,omitempty"`
}

func (m MongoDBCommunity) GetAgentPasswordSecretNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Name + "-agent-password", Namespace: m.Namespace}
}

func (m MongoDBCommunity) GetAgentKeyfileSecretNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Name + "-keyfile", Namespace: m.Namespace}
}

func (m MongoDBCommunity) GetOwnerReferences() []metav1.OwnerReference {
	ownerReference := *metav1.NewControllerRef(&m, schema.GroupVersionKind{
		Group:   GroupVersion.Group,
		Version: GroupVersion.Version,
		Kind:    m.Kind,
	})
	return []metav1.OwnerReference{ownerReference}
}

// GetScramOptions returns a set of Options that are used to configure scram
// authentication.
func (m MongoDBCommunity) GetScramOptions() scram.Options {

	ignoreUnknownUsers := true
	if m.Spec.Security.Authentication.IgnoreUnknownUsers != nil {
		ignoreUnknownUsers = *m.Spec.Security.Authentication.IgnoreUnknownUsers
	}

	return scram.Options{
		AuthoritativeSet:   !ignoreUnknownUsers,
		KeyFile:            scram.AutomationAgentKeyFilePathInContainer,
		AutoAuthMechanisms: []string{scram.Sha256},
		AgentName:          scram.AgentName,
		AutoAuthMechanism:  scram.Sha256,
	}
}

// GetScramUsers converts all of the users from the spec into users
// that can be used to configure scram authentication.
func (m MongoDBCommunity) GetScramUsers() []scram.User {
	users := make([]scram.User, len(m.Spec.Users))
	for i, u := range m.Spec.Users {
		roles := make([]scram.Role, len(u.Roles))
		for j, r := range u.Roles {
			roles[j] = scram.Role{
				Name:     r.Name,
				Database: r.DB,
			}
		}
		users[i] = scram.User{
			Username:                   u.Name,
			Database:                   u.DB,
			Roles:                      roles,
			PasswordSecretKey:          u.GetPasswordSecretKey(),
			PasswordSecretName:         u.PasswordSecretRef.Name,
			ScramCredentialsSecretName: u.GetScramCredentialsSecretName(),
		}
	}
	return users
}

func (m MongoDBCommunity) AutomationConfigMembersThisReconciliation() int {
	// determine the correct number of automation config replica set members
	// based on our desired number, and our current number
	return scale.ReplicasThisReconciliation(automationConfigReplicasScaler{
		desired: m.Spec.Members,
		current: m.Status.CurrentMongoDBMembers,
	})
}

// MongoURI returns a mongo uri which can be used to connect to this deployment
func (m MongoDBCommunity) MongoURI() string {
	return fmt.Sprintf("mongodb://%s", strings.Join(m.Hosts(), ","))
}

// MongoSRVURI returns a mongo srv uri which can be used to connect to this deployment
func (m MongoDBCommunity) MongoSRVURI() string {
	clusterDomain := "svc.cluster.local" // TODO: make this configurable
	return fmt.Sprintf("mongodb+srv://%s.%s.%s", m.ServiceName(), m.Namespace, clusterDomain)
}

// MongoAuthUserURI returns a mongo uri which can be used to connect to this deployment
// and includes the authentication data for the user
func (m MongoDBCommunity) MongoAuthUserURI(user scram.User, password string) string {
	return fmt.Sprintf("mongodb://%s:%s@%s/%s?ssl=%t",
		url.QueryEscape(user.Username),
		url.QueryEscape(password),
		strings.Join(m.Hosts(), ","),
		user.Database,
		m.Spec.Security.TLS.Enabled)
}

// MongoAuthUserSRVURI returns a mongo srv uri which can be used to connect to this deployment
// and includes the authentication data for the user
func (m MongoDBCommunity) MongoAuthUserSRVURI(user scram.User, password string) string {
	clusterDomain := "svc.cluster.local" // TODO: make this configurable
	return fmt.Sprintf("mongodb+srv://%s:%s@%s.%s.%s/%s?ssl=%t",
		url.QueryEscape(user.Username),
		url.QueryEscape(password),
		m.ServiceName(),
		m.Namespace,
		clusterDomain,
		user.Database,
		m.Spec.Security.TLS.Enabled)
}

func (m MongoDBCommunity) Hosts() []string {
	hosts := make([]string, m.Spec.Members)
	clusterDomain := "svc.cluster.local" // TODO: make this configurable
	for i := 0; i < m.Spec.Members; i++ {
		hosts[i] = fmt.Sprintf("%s-%d.%s.%s.%s:%d", m.Name, i, m.ServiceName(), m.Namespace, clusterDomain, 27017)
	}
	return hosts
}

// ServiceName returns the name of the Service that should be created for
// this resource
func (m MongoDBCommunity) ServiceName() string {
	return m.Name + "-svc"
}

func (m MongoDBCommunity) AutomationConfigSecretName() string {
	return m.Name + "-config"
}

// TLSConfigMapNamespacedName will get the namespaced name of the ConfigMap containing the CA certificate
// As the ConfigMap will be mounted to our pods, it has to be in the same namespace as the MongoDB resource
func (m MongoDBCommunity) TLSConfigMapNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Spec.Security.TLS.CaConfigMap.Name, Namespace: m.Namespace}
}

// TLSSecretNamespacedName will get the namespaced name of the Secret containing the server certificate and key
func (m MongoDBCommunity) TLSSecretNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Spec.Security.TLS.CertificateKeySecret.Name, Namespace: m.Namespace}
}

// TLSOperatorSecretNamespacedName will get the namespaced name of the Secret created by the operator
// containing the combined certificate and key.
func (m MongoDBCommunity) TLSOperatorSecretNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Name + "-server-certificate-key", Namespace: m.Namespace}
}

func (m MongoDBCommunity) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: m.Name, Namespace: m.Namespace}
}

func (m MongoDBCommunity) GetAgentScramCredentialsNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: fmt.Sprintf("%s-agent-scram-credentials", m.Name), Namespace: m.Namespace}
}

func (m MongoDBCommunity) DesiredReplicas() int {
	return m.Spec.Members
}

func (m MongoDBCommunity) CurrentReplicas() int {
	return m.Status.CurrentStatefulSetReplicas
}

func (m MongoDBCommunity) GetMongoDBVersion() string {
	return m.Spec.Version
}

// GetMongoDBVersionForAnnotation returns the MDB version used to annotate the object.
// Here it's the same as GetMongoDBVersion, but a different name is used in order to make
// the usage clearer in enterprise (where it's a method of OpsManager but is used for the AppDB)
func (m MongoDBCommunity) GetMongoDBVersionForAnnotation() string {
	return m.GetMongoDBVersion()
}

func (m *MongoDBCommunity) StatefulSetReplicasThisReconciliation() int {
	return scale.ReplicasThisReconciliation(m)
}

// GetUpdateStrategyType returns the type of RollingUpgradeStrategy that the
// MongoDB StatefulSet should be configured with.
func (m MongoDBCommunity) GetUpdateStrategyType() appsv1.StatefulSetUpdateStrategyType {
	if !m.IsChangingVersion() {
		return appsv1.RollingUpdateStatefulSetStrategyType
	}
	return appsv1.OnDeleteStatefulSetStrategyType
}

// IsChangingVersion returns true if an attempted version change is occurring.
func (m MongoDBCommunity) IsChangingVersion() bool {
	prevVersion := m.getPreviousVersion()
	return prevVersion != "" && prevVersion != m.Spec.Version
}

// GetPreviousVersion returns the last MDB version the statefulset was configured with.
func (m MongoDBCommunity) getPreviousVersion() string {
	return annotations.GetAnnotation(&m, annotations.LastAppliedMongoDBVersion)
}

func (m MongoDBCommunity) HasSeparateDataAndLogsVolumes() bool {
	return true
}

func (m MongoDBCommunity) GetAnnotations() map[string]string {
	return m.Annotations
}

func (m MongoDBCommunity) DataVolumeName() string {
	return "data-volume"
}

func (m MongoDBCommunity) LogsVolumeName() string {
	return "logs-volume"
}

type automationConfigReplicasScaler struct {
	current, desired int
}

func (a automationConfigReplicasScaler) DesiredReplicas() int {
	return a.desired
}

func (a automationConfigReplicasScaler) CurrentReplicas() int {
	return a.current
}

// +kubebuilder:object:root=true

// MongoDBCommunityList contains a list of MongoDB
type MongoDBCommunityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MongoDBCommunity `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MongoDBCommunity{}, &MongoDBCommunityList{})
}
