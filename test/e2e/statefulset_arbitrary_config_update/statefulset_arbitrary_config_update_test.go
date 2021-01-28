package statefulset_arbitrary_config

import (
	"testing"

	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/util/mongotester"

	v1 "github.com/mongodb/mongodb-kubernetes-operator/pkg/apis/mongodb/v1"
	e2eutil "github.com/mongodb/mongodb-kubernetes-operator/test/e2e"
	"github.com/mongodb/mongodb-kubernetes-operator/test/e2e/mongodbtests"
	setup "github.com/mongodb/mongodb-kubernetes-operator/test/e2e/setup"
	f "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestMain(m *testing.M) {
	f.MainEntry(m)
}

func TestStatefulSetArbitraryConfig(t *testing.T) {
	ctx, shouldCleanup := setup.InitTest(t)

	if shouldCleanup {
		defer ctx.Cleanup()
	}
	mdb, user := e2eutil.NewTestMongoDB("mdb0", "")

	_, err := setup.GeneratePasswordForUser(user, ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	tester, err := mongotester.FromResource(t, mdb)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Create MongoDB Resource", mongodbtests.CreateMongoDBResource(&mdb, ctx))
	t.Run("Basic tests", mongodbtests.BasicFunctionality(&mdb))
	t.Run("Test basic connectivity", tester.ConnectivitySucceeds())
	t.Run("AutomationConfig has the correct version", mongodbtests.AutomationConfigVersionHasTheExpectedVersion(&mdb, 1))

	overrideSpec := v1.StatefulSetConfiguration{}
	overrideSpec.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "mongodb-agent", ReadinessProbe: &corev1.Probe{TimeoutSeconds: 100}}}

	err = e2eutil.UpdateMongoDBResource(&mdb, func(mdb *v1.MongoDBCommunity) { mdb.Spec.StatefulSetConfiguration = overrideSpec })
	assert.NoError(t, err)

	t.Run("Basic tests after update", mongodbtests.BasicFunctionality(&mdb))
	t.Run("Test basic connectivity after update", tester.ConnectivitySucceeds())
	t.Run("Container has been merged by name", mongodbtests.StatefulSetContainerConditionIsTrue(&mdb, "mongodb-agent", func(container corev1.Container) bool {
		return container.ReadinessProbe.TimeoutSeconds == 100
	}))
}
