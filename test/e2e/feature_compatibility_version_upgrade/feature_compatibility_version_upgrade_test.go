package feature_compatibility_version_upgrade

import (
	"testing"
	"time"

	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/util/mongotester"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/pkg/apis/mongodb/v1"
	e2eutil "github.com/mongodb/mongodb-kubernetes-operator/test/e2e"
	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/mongodbtests"
	setup "github.com/mongodb/mongodb-kubernetes-operator/test/e2e/setup"
	f "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	f.MainEntry(m)
}

func TestFeatureCompatibilityVersionUpgrade(t *testing.T) {

	ctx, shouldCleanup := setup.InitTest(t)

	if shouldCleanup {
		defer ctx.Cleanup()
	}

	mdb, user := e2eutil.NewTestMongoDB("mdb0", "")
	mdb.Spec.Version = "4.0.6"
	mdb.Spec.FeatureCompatibilityVersion = "4.0"

	_, err := setup.GeneratePasswordForUser(user, ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = setup.GeneratePasswordForUser(user, ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	tester, err := mongotester.FromResource(t, mdb)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Create MongoDB Resource", mongodbtests.CreateMongoDBResource(&mdb, ctx))
	t.Run("Basic tests", mongodbtests.BasicFunctionality(&mdb))
	t.Run("Ensure Authentication", tester.EnsureAuthenticationIsConfigured(3))
	t.Run("Test FeatureCompatibilityVersion is 4.0", tester.HasFCV("4.0", 3))

	// Upgrade version to 4.2.6 while keeping the FCV set to 4.0
	t.Run("MongoDB is reachable", func(t *testing.T) {
		defer tester.StartBackgroundConnectivityTest(t, time.Second*10)()
		t.Run("Test Version can be upgraded", mongodbtests.ChangeVersion(&mdb, "4.2.6"))
		t.Run("Stateful Set Reaches Ready State, after Upgrading", mongodbtests.StatefulSetIsReady(&mdb))
		t.Run("Test Basic Connectivity after upgrade has completed", tester.ConnectivitySucceeds())
	})

	t.Run("Test FeatureCompatibilityVersion, after upgrade, is 4.0", tester.HasFCV("4.0", 3))

	t.Run("MongoDB is reachable", func(t *testing.T) {
		t.Run("Test FCV can be upgraded", func(t *testing.T) {
			err := e2eutil.UpdateMongoDBResource(&mdb, func(db *mdbv1.MongoDBCommunity) {
				db.Spec.FeatureCompatibilityVersion = "4.2"
			})
			assert.NoError(t, err)
		})
		t.Run("Stateful Set Reaches Ready State", mongodbtests.StatefulSetIsReady(&mdb))
		t.Run("MongoDB Reaches Running Phase", mongodbtests.MongoDBReachesRunningPhase(&mdb))
	})
	t.Run("Test FeatureCompatibilityVersion, after upgrade, is 4.2", tester.HasFCV("4.2", 3))
}
