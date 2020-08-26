package replica_set_tls

import (
	"testing"
	"time"

	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/tlstests"

	e2eutil "github.com/mongodb/mongodb-kubernetes-operator/test/e2e"
	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/mongodbtests"
	setup "github.com/mongodb/mongodb-kubernetes-operator/test/e2e/setup"
	f "github.com/operator-framework/operator-sdk/pkg/test"
)

func TestMain(m *testing.M) {
	f.MainEntry(m)
}

func TestReplicaSetTLSRotate(t *testing.T) {
	ctx, shouldCleanup := setup.InitTest(t)
	if shouldCleanup {
		defer ctx.Cleanup()
	}

	mdb, user := e2eutil.NewTestMongoDB("mdb-tls")
	mdb.Spec.Security.TLS = e2eutil.NewTestTLSConfig(false)

	password, err := setup.GeneratePasswordForUser(user, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := setup.CreateTLSResources(mdb.Namespace, ctx); err != nil {
		t.Fatalf("Failed to set up TLS resources: %s", err)
	}

	t.Run("Create MongoDB Resource", mongodbtests.CreateMongoDBResource(&mdb, ctx))
	t.Run("Basic tests", mongodbtests.BasicFunctionality(&mdb))
	t.Run("Wait for TLS to be enabled", tlstests.WaitForTLSMode(&mdb, "requireSSL", user.Name, password))
	t.Run("Test Basic TLS Connectivity", tlstests.ConnectivityWithTLS(&mdb, user.Name, password))
	t.Run("Test TLS required", tlstests.ConnectivityWithoutTLSShouldFail(&mdb, user.Name, password))

	t.Run("MongoDB is reachable while certificate is rotated", tlstests.IsReachableOverTLSDuring(&mdb, time.Second*10, user.Name, password,
		func() {
			t.Run("Update certificate secret", tlstests.RotateCertificate(&mdb))
			t.Run("Wait for certificate to be rotated", tlstests.WaitForRotatedCertificate(&mdb))
		},
	))
}
