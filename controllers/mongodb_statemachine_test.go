package controllers

import (
	"context"
	"encoding/json"
	"testing"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/client"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestOrderOfStates_NoTLS(t *testing.T) {
	mdb := newTestReplicaSet()

	mgr := client.NewManager(&mdb)
	r := NewReconciler(mgr)

	assertFullOrderOfStates(t, r, mdb,
		reconciliationStartStateName,
		validateSpecStateName,
		createServiceStateName,
		deployMongoDBReplicaSetStartName,
		deployAutomationConfigStateName,
		deployStatefulSetStateName,
		deployMongoDBReplicaSetEndName,
		updateStatusStateName,
		reconciliationEndStateName,
	)
}

func TestFullOrderOfStates_TLSEnabled(t *testing.T) {
	mdb := newTestReplicaSetWithTLS()

	mgr := client.NewManager(&mdb)

	err := createTLSSecretAndConfigMap(mgr.GetClient(), mdb)
	assert.NoError(t, err)

	r := NewReconciler(mgr)

	// if the StatefulSet does not exist, the automation config should be updated first.
	assertFullOrderOfStates(t, r, mdb,
		reconciliationStartStateName,
		validateSpecStateName,
		createServiceStateName,
		tlsValidationStateName,
		tlsResourcesStateName,
		deployMongoDBReplicaSetStartName,
		deployAutomationConfigStateName,
		deployStatefulSetStateName,
		deployMongoDBReplicaSetEndName,
		updateStatusStateName,
		reconciliationEndStateName,
	)

}

func TestPartialOrderOfStates_TLSEnabled(t *testing.T) {
	mdb := newTestReplicaSetWithTLS()

	mgr := client.NewManager(&mdb)

	err := createTLSSecretAndConfigMap(mgr.GetClient(), mdb)
	assert.NoError(t, err)

	r := NewReconciler(mgr)

	// if the StatefulSet does not exist, the automation config should be updated first.
	assertPartialOrderOfStates(t, r, mdb,
		deployAutomationConfigStateName,
		deployStatefulSetStateName,
	)

	// Once the StatefulSet exists, the stateful set should be updated first
	assertPartialOrderOfStates(t, r, mdb,
		deployStatefulSetStateName,
		deployAutomationConfigStateName,
	)

}

// reconcileThroughAllStates performs reconciliations until the final state has been reached. It asserts
// that no errors have occurred in any reconciliation.
func reconcileThroughAllStates(t *testing.T, r *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity) {
	res, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
	assert.NoError(t, err)
	for res.Requeue || res.RequeueAfter > 0 {
		res, err = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mdb.Namespace, Name: mdb.Name}})
		assert.NoError(t, err)
	}
}

// assertFullOrderOfStates asserts that the order of states traversed is exactly as specfied.
func assertFullOrderOfStates(t *testing.T, r *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity, states ...string) {
	reconcileThroughAllStates(t, r, mdb)

	stateHistory, err := getStateHistory(mdb)
	assert.NoError(t, err)

	assert.Equal(t, len(states), len(stateHistory))
	assert.Equal(t, states, stateHistory)
}

func getStateHistory(mdb mdbv1.MongoDBCommunity) ([]string, error) {
	allStates := MongoDBStates{}
	bytes := []byte(mdb.Annotations[stateMachineAnnotation])

	if err := json.Unmarshal(bytes, &allStates); err != nil {
		return nil, err
	}

	// FIXME: currently the very first state is not saved
	return append([]string{reconciliationStartStateName}, allStates.StateHistory...), nil
}

// assertPartialOrderOfStates assert whether or not the subset of states exists in that order in the full history of states traversed.
func assertPartialOrderOfStates(t *testing.T, r *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity, subsetOfStates ...string) {
	reconcileThroughAllStates(t, r, mdb)

	stateHistory, err := getStateHistory(mdb)
	assert.NoError(t, err)

	startingIndex := -1
	for i, historyState := range stateHistory {
		if historyState == subsetOfStates[0] {
			startingIndex = i
			break
		}
	}

	if startingIndex == -1 {
		t.Fatalf("Subset %v did not exist within %v", subsetOfStates, stateHistory)
	}

	var match []string
	for i := startingIndex; i < startingIndex+len(subsetOfStates); i++ {
		// prevent OOB
		if i >= len(stateHistory) {
			break
		}
		match = append(match, stateHistory[i])
	}

	assert.Equal(t, subsetOfStates, match)
}
