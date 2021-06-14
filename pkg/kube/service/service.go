package service

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Getter interface {
	GetService(objectKey client.ObjectKey) (corev1.Service, error)
}

type Updater interface {
	UpdateService(service corev1.Service) error
}

type Creator interface {
	CreateService(service corev1.Service) error
}

type Deleter interface {
	DeleteService(objectKey client.ObjectKey) error
}

type GetDeleter interface {
	Getter
	Deleter
}

type GetUpdater interface {
	Getter
	Updater
}

type GetUpdateCreator interface {
	Getter
	Updater
	Creator
}

type GetUpdateCreateDeleter interface {
	Getter
	Updater
	Creator
	Deleter
}

func DeleteServiceIfItExists(getterDeleter GetDeleter, serviceName types.NamespacedName) error {
	_, err := getterDeleter.GetService(serviceName)
	if err != nil {
		// If it is not found return
		if apiErrors.IsNotFound(err) {
			return nil
		}
		// Otherwise we got an error when trying to get it
		return fmt.Errorf("can't get service %s: %s", serviceName, err)
	}
	return getterDeleter.DeleteService(serviceName)
}

// Merge merges `source` into `dest`. Both arguments will remain unchanged
// a new service will be created and returned.
// The "merging" process is arbitrary and it only handle specific attributes
func Merge(dest corev1.Service, source corev1.Service) corev1.Service {
	for k, v := range source.ObjectMeta.Annotations {
		dest.ObjectMeta.Annotations[k] = v
	}

	for k, v := range source.ObjectMeta.Labels {
		dest.ObjectMeta.Labels[k] = v
	}

	var nodePort int32 = 0
	if len(dest.Spec.Ports) > 0 {
		// Save the NodePort for later, in case this ServicePort is changed.
		nodePort = dest.Spec.Ports[0].NodePort
	}

	if len(source.Spec.Ports) > 0 {
		dest.Spec.Ports = source.Spec.Ports

		if nodePort > 0 && source.Spec.Ports[0].NodePort == 0 {
			// There *is* a nodePort defined already, and a new one is not being passed
			dest.Spec.Ports[0].NodePort = nodePort
		}
	}

	dest.Spec.Type = source.Spec.Type
	dest.Spec.LoadBalancerIP = source.Spec.LoadBalancerIP
	dest.Spec.ExternalTrafficPolicy = source.Spec.ExternalTrafficPolicy
	return dest
}
