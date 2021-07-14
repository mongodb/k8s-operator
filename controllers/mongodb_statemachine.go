package controllers

var (
	stateMachineAnnotation = "mongodb.com/v1.stateMachine"

	reconciliationStartStateName = "ReconciliationStart"
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

// newStartingStates returns a MongoDBStates instance which will cause the State Machine
// to transition to the first state.
func newStartingStates() MongoDBStates {
	return MongoDBStates{
		NextState: reconciliationStartStateName,
	}
}
