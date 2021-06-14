package status

import (
	"testing"
	"time"

	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type errorOption struct{}

func (e errorOption) ApplyOption(_ *mdbv1.MongoDBCommunity) {}

func (e errorOption) GetResult() (reconcile.Result, error, bool) {
	return reconcile.Result{}, errors.Errorf("error"), false
}

type successOption struct{}

func (s successOption) ApplyOption(_ *mdbv1.MongoDBCommunity) {}

func (s successOption) GetResult() (reconcile.Result, error, bool) {
	return reconcile.Result{}, nil, true
}

type retryOption struct{}

func (r retryOption) ApplyOption(_ *mdbv1.MongoDBCommunity) {}

func (r retryOption) GetResult() (reconcile.Result, error, bool) {
	return reconcile.Result{Requeue: true}, nil, false
}

func TestDetermineReconciliationResult(t *testing.T) {

	t.Run("A single error option should result in an error return", func(t *testing.T) {
		opts := []Option{
			errorOption{},
			successOption{},
			successOption{},
		}

		res, err, _ := determineReconciliationResult(opts)
		assert.NotNil(t, err)
		assert.Equal(t, false, res.Requeue)
		assert.Equal(t, time.Duration(0), res.RequeueAfter)
	})

	t.Run("An error option takes precedence over a retry", func(t *testing.T) {
		opts := []Option{
			errorOption{},
			retryOption{},
			successOption{},
		}
		res, err, _ := determineReconciliationResult(opts)
		assert.NotNil(t, err)
		assert.Equal(t, false, res.Requeue)
		assert.Equal(t, time.Duration(0), res.RequeueAfter)
	})

	t.Run("No errors will result in a successful reconciliation", func(t *testing.T) {
		opts := []Option{
			successOption{},
			successOption{},
			successOption{},
		}
		res, err, _ := determineReconciliationResult(opts)
		assert.Nil(t, err)
		assert.Equal(t, false, res.Requeue)
		assert.Equal(t, time.Duration(0), res.RequeueAfter)
	})

	t.Run("A retry will take precedence over success", func(t *testing.T) {
		opts := []Option{
			successOption{},
			successOption{},
			retryOption{},
		}
		res, err, _ := determineReconciliationResult(opts)
		assert.Nil(t, err)
		assert.Equal(t, true, res.Requeue)
	})

}
