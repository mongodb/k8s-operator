package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"github.com/mongodb/mongodb-kubernetes-operator/controllers/watch"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/annotations"
	kubernetesClient "github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/client"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/statefulset"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/result"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/scale"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/state"
	"github.com/mongodb/mongodb-kubernetes-operator/pkg/util/status"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	stateMachineAnnotation = "mongodb.com/v1.stateMachine"

	reconciliationStartStateName            = "ReconciliationStart"
	ensureUserResourcesStateName            = "EnsureUserResources"
	createConnectionStringStateName         = "CreateConnectionString"
	validateSpecStateName                   = "ValidateSpec"
	createServiceStateName                  = "CreateService"
	tlsValidationStateName                  = "TLSValidation"
	tlsResourcesStateName                   = "EnsureTLSResources"
	deployMongoDBReplicaSetStartName        = "BeginDeployingMongoDBReplicaSet"
	deployMongoDBReplicaSetEndName          = "EndDeployingMongoDBReplicaSet"
	deployAutomationConfigStateName         = "DeployAutomationConfig"
	deployStatefulSetStateName              = "DeployStatefulSet"
	resetStatefulSetUpdateStrategyStateName = "ResetStatefulSetUpdateStrategy"
	reconciliationEndStateName              = "ReconciliationEnd"
	reconciliationRetryStateName            = "ReconciliationRetry"
	updateStatusStateName                   = "UpdateStatus"
)

// MongoDBStates stores information about state history and the
// next state that should be entered.
type MongoDBStates struct {
	NextState    string   `json:"nextState"`
	StateHistory []string `json:"stateHistory"`
}

func (m MongoDBStates) ContainsState(state string) bool {
	for _, s := range m.StateHistory {
		if s == state {
			return true
		}
	}
	return false
}

// BuildStateMachine creates a State Machine that is configured with all of the different transitions that are possible
// by the operator. A single state's Reconcile method is called when calling the Reconcile method of the State Machine.
func BuildStateMachine(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, secretWatcher *watch.ResourceWatcher, reconciler *ReplicaSetReconciler, log *zap.SugaredLogger) (*state.Machine, error) {
	sm := state.NewStateMachine(reconciler, mdb.NamespacedName(), log)

	needsToPublishStateFirst, reason := needToPublishStateFirst(client, mdb)

	reconciliationStart := NewReconciliationStartState(client, mdb, log)
	validateSpecState := NewValidateSpecState(client, reconciler, mdb, log)
	serviceCreationState := NewCreateServiceState(client, mdb, log)
	tlsValidationState := NewTLSValidationState(client, mdb, secretWatcher, log)
	tlsResourcesState := NewEnsureTLSResourcesState(client, mdb, log)
	deployMongoDBReplicaSetStartState := NewDeployMongoDBReplicaSetStartState(mdb, log)
	deployMongoDBReplicaSetEndState := NewDeployMongoDBReplicaSetEndState(mdb, log)
	deployAutomationConfigState := NewDeployAutomationConfigState(client, reason, mdb, log)
	deployStatefulSetState := NewDeployStatefulSetState(client, reason, mdb, log)
	resetUpdateStrategyState := NewResetStatefulSetUpdateStrategyState(client, mdb)
	updateStatusState := NewUpdateStatusState(client, mdb, log)
	endReconciliationState := NewReconciliationEndState(client, mdb, log)
	retryReconciliationState := NewRetryReconciliationState(client, mdb, log)
	ensureUsersState := NewEnsureUsersResourceState(reconciler, mdb, log)
	connectionStringSecretsState := NewCreateConnectionStringSecretState(reconciler, mdb, log)

	sm.AddDirectTransition(reconciliationStart, validateSpecState)

	sm.AddDirectTransition(validateSpecState, serviceCreationState)

	sm.AddDirectTransition(serviceCreationState, ensureUsersState)

	sm.AddTransition(ensureUsersState, tlsValidationState, state.FromBool(mdb.Spec.Security.TLS.Enabled))
	sm.AddDirectTransition(ensureUsersState, deployMongoDBReplicaSetStartState)

	sm.AddDirectTransition(tlsValidationState, tlsResourcesState)

	sm.AddDirectTransition(tlsResourcesState, deployMongoDBReplicaSetStartState)

	sm.AddTransition(deployMongoDBReplicaSetStartState, deployAutomationConfigState, state.FromBool(needsToPublishStateFirst))
	sm.AddTransition(deployMongoDBReplicaSetStartState, deployStatefulSetState, state.FromBool(!needsToPublishStateFirst))

	sm.AddTransition(deployStatefulSetState, deployAutomationConfigState, state.FromBool(!needsToPublishStateFirst))
	sm.AddDirectTransition(deployStatefulSetState, deployMongoDBReplicaSetEndState)

	sm.AddTransition(deployMongoDBReplicaSetEndState, resetUpdateStrategyState, mdb.IsChangingVersion)
	sm.AddDirectTransition(deployMongoDBReplicaSetEndState, connectionStringSecretsState)

	sm.AddTransition(deployAutomationConfigState, deployStatefulSetState, state.FromBool(needsToPublishStateFirst))
	sm.AddDirectTransition(deployAutomationConfigState, deployMongoDBReplicaSetEndState)

	sm.AddDirectTransition(resetUpdateStrategyState, connectionStringSecretsState)
	sm.AddDirectTransition(connectionStringSecretsState, updateStatusState)

	// if we're scaling, we should requeue a reconciliation. We can only scale MongoDB members one at a time,
	// so we repeat the whole reconciliation process per member we are scaling up/down.
	sm.AddTransition(updateStatusState, retryReconciliationState, state.FromBool(scale.IsStillScaling(&mdb)))
	sm.AddDirectTransition(updateStatusState, endReconciliationState)
	return sm, nil
}

// NewReconciliationStartState returns a State which resets the State Machine history and logs the current
// spec/status of the resource being reconciled.
func NewReconciliationStartState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: reconciliationStartStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := resetStateMachineHistory(client, mdb); err != nil {
				log.Errorf("Failed resetting StateMachine annotation: %s", err)
				return result.RetryState(5)
			}

			log.Infow("Reconciling MongoDB", "MongoDB.Spec", mdb.Spec, "MongoDB.Status", mdb.Status)
			return result.StateComplete()
		},
	}
}

// NewValidateSpecState performs validation on the Spec of the MongoDBCommunity resource.
func NewValidateSpecState(client kubernetesClient.Client, reconciler *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: validateSpecStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := reconciler.validateSpec(mdb); err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("error validating new Spec: %s", err)).
						withFailedPhase(),
				)
			}
			log.Debug("MongoDB.Spec was successfully validated.")
			return result.StateComplete()
		},
	}
}

// NewCreateServiceState ensures that the Service is created.
func NewCreateServiceState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: createServiceStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			log.Debug("Ensuring the service exists")
			if err := ensureService(client, mdb, log); err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error ensuring the service exists: %s", err)).
						withFailedPhase(),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewTLSValidationState validates the TLS components of the Spec of the resource.
func NewTLSValidationState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, secretWatcher *watch.ResourceWatcher, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: tlsValidationStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			isTLSValid, err := validateTLSConfig(client, mdb, secretWatcher, log)
			if err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error validating TLS config: %s", err)).
						withFailedPhase(),
				)
			}

			if !isTLSValid {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Info, "TLS config is not yet valid, retrying in 10 seconds").
						withPendingPhase(10),
				)
			}
			log.Debug("Successfully validated TLS configuration.")
			return result.StateComplete()
		},
	}
}

// NewEnsureTLSResourcesState ensures that all the required Kubernetes resources are valid for a TLS configuration.
func NewEnsureTLSResourcesState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: tlsResourcesStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := ensureTLSResources(client, mdb, log); err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error ensuring TLS resources: %s", err)).
						withFailedPhase(),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewDeployMongoDBReplicaSetStartState is the entry point to the deployment of the Automation Config
// and StatefulSet.
func NewDeployMongoDBReplicaSetStartState(mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: deployMongoDBReplicaSetStartName,
		Reconcile: func() (reconcile.Result, error, bool) {
			log.Infof("Deploying MongoDB ReplicaSet %s/%s", mdb.Namespace, mdb.Name)
			return result.StateComplete()
		},
	}
}

// NewDeployAutomationConfigState deploys the AutomationConfig.
func NewDeployAutomationConfigState(client kubernetesClient.Client, reason string, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: deployAutomationConfigStateName,
		OnEnter: func() error {
			if reason != "" {
				log.Debug(reason)
			}
			return nil
		},
		Reconcile: func() (reconcile.Result, error, bool) {
			ready, err := deployAutomationConfig(client, mdb, log)
			if err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error deploying Automation Config: %s", err)).
						withFailedPhase(),
				)
			}
			if !ready {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Info, "MongoDB agents are not yet ready, retrying in 10 seconds").
						withPendingPhase(10),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewDeployStatefulSetState deploys the StatefulSet.
func NewDeployStatefulSetState(client kubernetesClient.Client, reason string, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: deployStatefulSetStateName,
		OnEnter: func() error {
			if reason != "" {
				log.Debug(reason)
			}
			return nil
		},
		Reconcile: func() (reconcile.Result, error, bool) {
			ready, err := deployStatefulSet(client, mdb, log)

			// We just created the StatefulSet, if it is a not found error, just retry this state.
			if apiErrors.IsNotFound(err) {
				return result.RetryState(1)
			}

			if err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error deploying MongoDB StatefulSet: %s", err)).
						withFailedPhase(),
				)
			}

			if !ready {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Info, "StatefulSet is not yet ready, retrying in 10 seconds").
						withPendingPhase(10),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewDeployMongoDBReplicaSetEndState is the exit point to the deployment of the Automation Config
// and StatefulSet.
func NewDeployMongoDBReplicaSetEndState(mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: deployMongoDBReplicaSetEndName,
		Reconcile: func() (reconcile.Result, error, bool) {
			log.Infof("Finished deploying MongoDB ReplicaSet %s/%s", mdb.Namespace, mdb.Name)
			return result.StateComplete()
		},
	}
}

// NewResetStatefulSetUpdateStrategyState resets the UpdateStrategyType and is required for version upgrades.
func NewResetStatefulSetUpdateStrategyState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity) state.State {
	return state.State{
		Name: resetStatefulSetUpdateStrategyStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := statefulset.ResetUpdateStrategy(&mdb, client); err != nil {
				return status.Update(client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error resetting StatefulSet UpdateStrategyType: %s", err)).
						withFailedPhase(),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewUpdateStatusState updates the Status of the resource.
func NewUpdateStatusState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: updateStatusStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if scale.IsStillScaling(mdb) {

				// TODO: support pending phase also being a success
				// in terms of a state being successful.
				res, err, _ := status.Update(client.Status(), &mdb, statusOptions().
					withMongoDBMembers(mdb.AutomationConfigMembersThisReconciliation()).
					withMessage(Info, fmt.Sprintf("Performing scaling operation, currentMembers=%d, desiredMembers=%d",
						mdb.CurrentReplicas(), mdb.DesiredReplicas())).
					withStatefulSetReplicas(mdb.StatefulSetReplicasThisReconciliation()).
					withPendingPhase(10),
				)
				if err != nil {
					log.Errorf("Error updating the status of the MongoDB resource: %s", err)
					return res, err, false
				}
				return res, err, true
			}

			res, err, _ := status.Update(client.Status(), &mdb,
				statusOptions().
					withMongoURI(mdb.MongoURI()).
					withMongoDBMembers(mdb.AutomationConfigMembersThisReconciliation()).
					withStatefulSetReplicas(mdb.StatefulSetReplicasThisReconciliation()).
					withMessage(None, "").
					withRunningPhase(),
			)
			if err != nil {
				log.Errorf("Error updating the status of the MongoDB resource: %s", err)
				return res, err, false
			}

			// the last version will be duplicated in two annotations.
			// This is needed to reuse the update strategy logic in enterprise
			if err := annotations.UpdateLastAppliedMongoDBVersion(&mdb, client); err != nil {
				log.Errorf("Could not save current version as an annotation: %s", err)
			}
			if err := updateLastSuccessfulConfiguration(client, mdb); err != nil {
				log.Errorf("Could not save current spec as an annotation: %s", err)
			}

			return result.StateComplete()
		},
	}
}

// NewReconciliationEndState prepares the resource annotation for the next reconciliation.
func NewReconciliationEndState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: reconciliationEndStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := setNextState(client, mdb, reconciliationStartStateName); err != nil {
				log.Errorf("Failed resetting State Machine annotation: %s", err)
				return result.RetryState(1)
			}
			log.Infow("Successfully finished reconciliation", "MongoDB.Spec:", mdb.Spec, "MongoDB.Status:", mdb.Status)
			return result.OK()
		},
	}
}

// NewRetryReconciliationState prepares the resource annotation for the next reconciliation. This state should be
// used if the intended path is a full retry of the reconciliation loop starting from the beginning.
func NewRetryReconciliationState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: reconciliationRetryStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := setNextState(client, mdb, reconciliationStartStateName); err != nil {
				log.Errorf("Failed resetting State Machine annotation: %s", err)
				return result.RetryState(5)
			}
			log.Infow("Requeuing reconciliation")
			return result.RetryState(5)
		},
	}
}

// NewEnsureUsersResourceState returns a state which ensures that all user kubernetes resources are created.
func NewEnsureUsersResourceState(r *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: ensureUserResourcesStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := r.ensureUserResources(mdb, log); err != nil {
				return status.Update(r.client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Error ensuring User config: %s", err)).
						withFailedPhase(),
				)
			}
			return result.StateComplete()
		},
	}
}

// NewCreateConnectionStringSecretState creates connect strings for the users of the given resource.
func NewCreateConnectionStringSecretState(r *ReplicaSetReconciler, mdb mdbv1.MongoDBCommunity, log *zap.SugaredLogger) state.State {
	return state.State{
		Name: createConnectionStringStateName,
		Reconcile: func() (reconcile.Result, error, bool) {
			if err := r.updateConnectionStringSecrets(mdb, log); err != nil {
				return status.Update(r.client.Status(), &mdb,
					statusOptions().
						withMessage(Error, fmt.Sprintf("Could not update connection string secrets: %s", err)).
						withFailedPhase(),
				)
			}
			return result.StateComplete()
		},
	}
}

// newStartingStates returns a MongoDBStates instance which will cause the State Machine
// to transition to the first state.
func newStartingStates() MongoDBStates {
	return MongoDBStates{
		NextState: reconciliationStartStateName,
	}
}

// setNextState updates the StateMachine annotation to indicate that it should transition to the
// given state next.
func setNextState(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity, stateName string) error {
	allStates, err := getExistingStateMachineStatesFromAnnotation(mdb)
	if err != nil {
		return err
	}
	allStates.NextState = stateName

	bytes, err := json.Marshal(allStates)
	if err != nil {
		return err
	}

	annotations.SetAnnotation(&mdb, stateMachineAnnotation, string(bytes))
	return client.Update(context.TODO(), &mdb)
}

// resetStateMachineHistory resets the history of states that have occurred. This should happen
// at the beginning of the first reconciliation.
func resetStateMachineHistory(client kubernetesClient.Client, mdb mdbv1.MongoDBCommunity) error {
	allStates, err := getExistingStateMachineStatesFromAnnotation(mdb)
	if err != nil {
		return err
	}
	allStates.StateHistory = []string{}

	bytes, err := json.Marshal(allStates)
	if err != nil {
		return err
	}

	annotations.SetAnnotation(&mdb, stateMachineAnnotation, string(bytes))
	return client.Update(context.TODO(), &mdb)
}
