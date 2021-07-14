package mongodbcommunity

import (
	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Getter interface {
	GetMongoDBCommunity(objectKey client.ObjectKey) (mdbv1.MongoDBCommunity, error)
}
