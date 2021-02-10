package scram

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/automationconfig"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/generate"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/authentication/scramcredentials"
	"go.uber.org/zap"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/secret"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
}

const (
	testSha1Salt      = "zEt5uDSnr/l9paFPsQzhAA=="
	testSha1ServerKey = "LEm/fv4gM0Y/XizbUoz/hULRnX0="
	testSha1StoredKey = "0HzXK7NtK40HXVn6zOqrNKVl+MY="

	testSha256Salt      = "qRr+7VgicfVcFjwZhu8u5JSE5ZeVBUP1A+lM4A=="
	testSha256ServerKey = "C9FIUhP6mqwe/2SJIheGBpOIqlxuq9Nh3fs+t+R/3zk="
	testSha256StoredKey = "7M7dUSY0sHTOXdNnoPSVbXg9Flon1b3t8MINGI8Tst0="
)

func newMockedSecretGetUpdateCreateDeleter(secrets ...corev1.Secret) secret.GetUpdateCreateDeleter {
	mockSecretGetUpdateCreateDeleter := mockSecretGetUpdateCreateDeleter{}
	mockSecretGetUpdateCreateDeleter.secrets = make(map[client.ObjectKey]corev1.Secret)
	for _, s := range secrets {
		mockSecretGetUpdateCreateDeleter.secrets[types.NamespacedName{Name: s.Name, Namespace: s.Namespace}] = s
	}
	return mockSecretGetUpdateCreateDeleter
}
func notFoundError() error {
	return &errors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonNotFound}}
}

func TestReadExistingCredentials(t *testing.T) {
	mdbObjectKey := types.NamespacedName{Name: "mdb-0", Namespace: "default"}
	user := buildMongoDBUser("mdbuser-0")
	t.Run("credentials are successfully generated when all fields are present", func(t *testing.T) {
		scramCredsSecret := validScramCredentialsSecret(mdbObjectKey, user.GetScramCredentialsSecretName())

		scram1Creds, scram256Creds, err := readExistingCredentials(newMockedSecretGetUpdateCreateDeleter(scramCredsSecret), mdbObjectKey, user.GetScramCredentialsSecretName())
		assert.NoError(t, err)
		assertScramCredsCredentialsValidity(t, scram1Creds, scram256Creds)
	})

	t.Run("credentials are not generated if a field is missing", func(t *testing.T) {
		scramCredsSecret := invalidSecret(mdbObjectKey, user.GetScramCredentialsSecretName())
		_, _, err := readExistingCredentials(newMockedSecretGetUpdateCreateDeleter(scramCredsSecret), mdbObjectKey, user.GetScramCredentialsSecretName())
		assert.Error(t, err)
	})

	t.Run("credentials are not generated if the secret does not exist", func(t *testing.T) {
		scramCredsSecret := validScramCredentialsSecret(mdbObjectKey, user.GetScramCredentialsSecretName())
		_, _, err := readExistingCredentials(newMockedSecretGetUpdateCreateDeleter(scramCredsSecret), mdbObjectKey, "different-username")
		assert.Error(t, err)
	})

}

func TestComputeScramCredentials_ComputesSameStoredAndServerKey_WithSameSalt(t *testing.T) {
	sha1Salt, sha256SaltKey, err := generate.Salts()
	assert.NoError(t, err)

	username := "user-1"
	password := "X6oSVAfD1la8fJwhfN" // nolint

	for i := 0; i < 10; i++ {
		sha1Creds0, sha256Creds0, err := computeScramShaCredentials(username, password, sha1Salt, sha256SaltKey)
		assert.NoError(t, err)
		sha1Creds1, sha256Creds1, err := computeScramShaCredentials(username, password, sha1Salt, sha256SaltKey)
		assert.NoError(t, err)

		assert.True(t, reflect.DeepEqual(sha1Creds0, sha1Creds1))
		assert.True(t, reflect.DeepEqual(sha256Creds0, sha256Creds1))
	}
}

func TestEnsureScramCredentials(t *testing.T) {
	mdb, user := buildConfigurableAndUser("mdb-0")
	t.Run("Fails when there is no password secret, and no credentials secret", func(t *testing.T) {
		_, _, err := ensureScramCredentials(newMockedSecretGetUpdateCreateDeleter(), user, mdb.NamespacedName())
		assert.Error(t, err)
	})
	t.Run("Existing credentials are used when password does not exist, but credentials secret has been created", func(t *testing.T) {
		scramCredentialsSecret := validScramCredentialsSecret(mdb.NamespacedName(), user.GetScramCredentialsSecretName())
		scram1Creds, scram256Creds, err := ensureScramCredentials(newMockedSecretGetUpdateCreateDeleter(scramCredentialsSecret), user, mdb.NamespacedName())
		assert.NoError(t, err)
		assertScramCredsCredentialsValidity(t, scram1Creds, scram256Creds)
	})
	t.Run("Changing password results in different credentials being returned", func(t *testing.T) {
		newPassword, err := generate.RandomFixedLengthStringOfSize(20)
		assert.NoError(t, err)

		differentPasswordSecret := secret.Builder().
			SetName(user.GetPasswordSecretName()).
			SetNamespace(mdb.NamespacedName().Namespace).
			SetField(user.GetPasswordSecretKey(), newPassword).
			Build()

		scramCredentialsSecret := validScramCredentialsSecret(mdb.NamespacedName(), user.GetScramCredentialsSecretName())
		scram1Creds, scram256Creds, err := ensureScramCredentials(newMockedSecretGetUpdateCreateDeleter(scramCredentialsSecret, differentPasswordSecret), user, mdb.NamespacedName())
		assert.NoError(t, err)
		assert.NotEqual(t, testSha1Salt, scram1Creds.Salt)
		assert.NotEmpty(t, scram1Creds.Salt)
		assert.NotEqual(t, testSha1StoredKey, scram1Creds.StoredKey)
		assert.NotEmpty(t, scram1Creds.StoredKey)
		assert.NotEqual(t, testSha1StoredKey, scram1Creds.ServerKey)
		assert.NotEmpty(t, scram1Creds.ServerKey)
		assert.Equal(t, 10000, scram1Creds.IterationCount)

		assert.NotEqual(t, testSha256Salt, scram256Creds.Salt)
		assert.NotEmpty(t, scram256Creds.Salt)
		assert.NotEqual(t, testSha256StoredKey, scram256Creds.StoredKey)
		assert.NotEmpty(t, scram256Creds.StoredKey)
		assert.NotEqual(t, testSha256ServerKey, scram256Creds.ServerKey)
		assert.NotEmpty(t, scram256Creds.ServerKey)
		assert.Equal(t, 15000, scram256Creds.IterationCount)
	})

}

func TestConvertMongoDBUserToAutomationConfigUser(t *testing.T) {
	mdb, user := buildConfigurableAndUser("mdb-0")

	t.Run("When password exists, the user is created in the automation config", func(t *testing.T) {
		passwordSecret := secret.Builder().
			SetName(user.GetPasswordSecretName()).
			SetNamespace(mdb.NamespacedName().Namespace).
			SetField(user.GetPasswordSecretKey(), "TDg_DESiScDrJV6").
			Build()

		acUser, err := convertMongoDBUserToAutomationConfigUser(newMockedSecretGetUpdateCreateDeleter(passwordSecret), mdb.NamespacedName(), user)

		assert.NoError(t, err)
		assert.Equal(t, user.GetUsername(), acUser.Username)
		assert.Equal(t, user.GetDatabase(), "admin")
		assert.Equal(t, len(user.GetScramRoles()), len(acUser.Roles))
		assert.NotNil(t, acUser.ScramSha1Creds)
		assert.NotNil(t, acUser.ScramSha256Creds)
		for i, acRole := range acUser.Roles {
			assert.Equal(t, user.GetScramRoles()[i].GetName(), acRole.Role)
			assert.Equal(t, user.GetScramRoles()[i].GetDatabase(), acRole.Database)
		}
	})

	t.Run("If there is no password secret, the creation fails", func(t *testing.T) {
		_, err := convertMongoDBUserToAutomationConfigUser(newMockedSecretGetUpdateCreateDeleter(), mdb.NamespacedName(), user)
		assert.Error(t, err)
	})
}

func TestEnsureEnabler(t *testing.T) {
	t.Run("Should fail if there is no password present for the user", func(t *testing.T) {
		mdb, _ := buildConfigurableAndUser("mdb-0")
		s := newMockedSecretGetUpdateCreateDeleter()

		auth := automationconfig.Auth{}
		err := Enable(&auth, s, mdb)
		assert.Error(t, err)
	})
	t.Run("Agent Credentials Secret should be created if there are no users", func(t *testing.T) {
		mdb := buildConfigurable("mdb-0")
		s := newMockedSecretGetUpdateCreateDeleter()
		auth := automationconfig.Auth{}
		err := Enable(&auth, s, mdb)
		assert.NoError(t, err)

		agentCredentialsSecret, err := s.GetSecret(mdb.GetAgentScramCredentialsNamespacedName())
		assert.NoError(t, err)
		assert.True(t, secret.HasAllKeys(agentCredentialsSecret, AgentKeyfileKey, AgentPasswordKey))
		assert.NotEmpty(t, agentCredentialsSecret.Data[AgentPasswordKey])
		assert.NotEmpty(t, agentCredentialsSecret.Data[AgentKeyfileKey])
	})

	t.Run("Agent Secret is used if it exists", func(t *testing.T) {
		mdb := buildConfigurable("mdb-0")

		agentPasswordSecret := secret.Builder().
			SetName(mdb.GetAgentScramCredentialsNamespacedName().Name).
			SetNamespace(mdb.GetAgentScramCredentialsNamespacedName().Namespace).
			SetField(AgentPasswordKey, "A21Zv5agv3EKXFfM").
			SetField(AgentKeyfileKey, "RuPeMaIe2g0SNTTa").
			Build()

		s := newMockedSecretGetUpdateCreateDeleter(agentPasswordSecret)
		auth := automationconfig.Auth{}
		err := Enable(&auth, s, mdb)
		assert.NoError(t, err)

		agentCredentialsSecret, err := s.GetSecret(mdb.GetAgentScramCredentialsNamespacedName())
		assert.NoError(t, err)
		assert.True(t, secret.HasAllKeys(agentCredentialsSecret, AgentKeyfileKey, AgentPasswordKey))
		assert.Equal(t, "A21Zv5agv3EKXFfM", string(agentCredentialsSecret.Data[AgentPasswordKey]))
		assert.Equal(t, "RuPeMaIe2g0SNTTa", string(agentCredentialsSecret.Data[AgentKeyfileKey]))

	})

	t.Run("Agent Credentials Secret should be created", func(t *testing.T) {
		mdb := buildConfigurable("mdb-0")
		s := newMockedSecretGetUpdateCreateDeleter()
		auth := automationconfig.Auth{}
		err := Enable(&auth, s, mdb)
		assert.NoError(t, err)
	})
}

func buildConfigurable(name string, users ...User) Configurable {
	return mockConfigurable{
		opts:  Options{},
		users: users,
		nsName: types.NamespacedName{
			Name:      name,
			Namespace: "default",
		},
	}
}

func buildMongoDBUser(name string) User {
	return mockUser{
		username: fmt.Sprintf("%s-user", name),
		database: "admin",
		roles: []Role{
			mockRole{
				name:     "readWrite",
				database: "testing",
			},
			mockRole{
				database: "testing",
				name:     "clusterAdmin",
			},
			// admin roles for reading FCV
			mockRole{
				database: "admin",
				name:     "readWrite",
			},
			mockRole{
				database: "admin",
				name:     "clusterAdmin",
			},
		},
		passwordSecretKey:  fmt.Sprintf("%s-password", name),
		passwordSecretName: fmt.Sprintf("%s-password-secret", name),
	}

}

func buildConfigurableAndUser(name string) (Configurable, User) {
	mdb := buildConfigurable(name, mockUser{
		username: fmt.Sprintf("%s-user", name),
		database: "admin",
		roles: []Role{
			mockRole{
				name:     "testing",
				database: "readWrite",
			},
			mockRole{
				database: "testing",
				name:     "clusterAdmin",
			},
			// admin roles for reading FCV
			mockRole{
				database: "admin",
				name:     "readWrite",
			},
			mockRole{
				database: "admin",
				name:     "clusterAdmin",
			},
		},
		passwordSecretKey:  fmt.Sprintf("%s-password", name),
		passwordSecretName: fmt.Sprintf("%s-password-secret", name),
	})
	return mdb, mdb.GetScramUsers()[0]
}

func assertScramCredsCredentialsValidity(t *testing.T, scram1Creds, scram256Creds scramcredentials.ScramCreds) {
	assert.Equal(t, testSha1Salt, scram1Creds.Salt)
	assert.Equal(t, testSha1StoredKey, scram1Creds.StoredKey)
	assert.Equal(t, testSha1ServerKey, scram1Creds.ServerKey)
	assert.Equal(t, 10000, scram1Creds.IterationCount)

	assert.Equal(t, testSha256Salt, scram256Creds.Salt)
	assert.Equal(t, testSha256StoredKey, scram256Creds.StoredKey)
	assert.Equal(t, testSha256ServerKey, scram256Creds.ServerKey)
	assert.Equal(t, 15000, scram256Creds.IterationCount)
}

// validScramCredentialsSecret returns a secret that has all valid scram credentials
func validScramCredentialsSecret(objectKey types.NamespacedName, scramCredentialsSecretName string) corev1.Secret {
	return secret.Builder().
		SetName(scramCredentialsSecretName).
		SetNamespace(objectKey.Namespace).
		SetField(sha1SaltKey, testSha1Salt).
		SetField(sha1StoredKeyKey, testSha1StoredKey).
		SetField(sha1ServerKeyKey, testSha1ServerKey).
		SetField(sha256SaltKey, testSha256Salt).
		SetField(sha256StoredKeyKey, testSha256StoredKey).
		SetField(sha256ServerKeyKey, testSha256ServerKey).
		Build()
}

// invalidSecret returns a secret that is incomplete
func invalidSecret(objectKey types.NamespacedName, scramCredentialsSecretName string) corev1.Secret {
	return secret.Builder().
		SetName(scramCredentialsSecretName).
		SetNamespace(objectKey.Namespace).
		SetField(sha1SaltKey, "nxBSYyZZIBZxStyt").
		SetField(sha1StoredKeyKey, "Bs4sePK0cdMy6n").
		SetField(sha1ServerKeyKey, "eP6_p76ql_h8iiH").
		Build()
}
