package feature_compatibility_version

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/util/wait"

	. "github.com/mongodb/mongodb-kubernetes-operator/test/e2e/util/mongotester"

	e2eutil "github.com/mongodb/mongodb-kubernetes-operator/test/e2e"
	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/mongodbtests"
	setup "github.com/mongodb/mongodb-kubernetes-operator/test/e2e/setup"
)

func TestMain(m *testing.M) {
	code, err := e2eutil.RunTest(m)
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(code)
}

func TestFeatureCompatibilityVersion(t *testing.T) {

	ctx := setup.Setup(t)
	defer ctx.Teardown()

	mdb, user := e2eutil.NewTestMongoDB(ctx, "mdb0", "")
	mdb.Spec.Version = "4.0.6"
	mdb.Spec.FeatureCompatibilityVersion = "4.0"

	_, err := setup.GeneratePasswordForUser(ctx, user, "")
	if err != nil {
		t.Fatal(err)
	}

	tester, err := FromResource(t, mdb)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Create MongoDB Resource", mongodbtests.CreateMongoDBResource(&mdb, ctx))
	t.Run("Basic tests", mongodbtests.BasicFunctionality(&mdb))
	t.Run("Ensure Authentication", tester.EnsureAuthenticationIsConfigured(3))
	t.Run("Test FeatureCompatibilityVersion is 4.0", tester.HasFCV("4.0", 3))

	// Upgrade version to 4.2.6 while keeping the FCV set to 4.0
	t.Run("MongoDB is reachable while version is upgraded", func(t *testing.T) {
		defer tester.StartBackgroundConnectivityTest(t, time.Second*10)()
		t.Run("Test Version can be upgraded", mongodbtests.ChangeVersion(&mdb, "4.2.6"))
		t.Run("Stateful Set Reaches Ready State, after Upgrading", mongodbtests.StatefulSetBecomesReady(&mdb, wait.Timeout(20*time.Minute)))
	})

	t.Run("Test Basic Connectivity after upgrade has completed", tester.ConnectivitySucceeds())
	t.Run("Test FeatureCompatibilityVersion, after upgrade, is 4.0", tester.HasFCV("4.0", 3))

	// Downgrade version back to 4.0.6, checks that the FeatureCompatibilityVersion stayed at 4.0
	t.Run("MongoDB is reachable while version is downgraded", func(t *testing.T) {
		defer tester.StartBackgroundConnectivityTest(t, time.Second*10)()
		t.Run("Test Version can be downgraded", mongodbtests.ChangeVersion(&mdb, "4.0.6"))
		t.Run("Stateful Set Reaches Ready State, after Upgrading", mongodbtests.StatefulSetBecomesReady(&mdb, wait.Timeout(20*time.Minute)))
	})

	t.Run("Test FeatureCompatibilityVersion, after downgrade, is 4.0", tester.HasFCV("4.0", 3))
}
