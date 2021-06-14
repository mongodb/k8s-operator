package client

import (
	"context"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// MockedManager exists to unit test the reconciliation loops and wrap the mocked client
type MockedManager struct {
	Client Client
}

func NewManager(obj k8sClient.Object) *MockedManager {
	c := NewMockedClient()
	if obj != nil {
		_ = c.Create(context.TODO(), obj)
	}
	return &MockedManager{Client: NewClient(c)}
}

func NewManagerWithClient(c k8sClient.Client) *MockedManager {
	return &MockedManager{Client: NewClient(c)}
}

func (m *MockedManager) Add(_ manager.Runnable) error {
	return nil
}

func (m *MockedManager) Elected() <-chan struct{} {
	return nil
}

// SetFields will set any dependencies on an object for which the object has implemented the inject
// interface - e.g. inject.Client.
func (m *MockedManager) SetFields(interface{}) error {
	return nil
}

// Start starts all registered Controllers and blocks until the Stop channel is closed.
// Returns an error if there is an error starting any controller.
func (m *MockedManager) Start(context.Context) error {
	return nil
}

// GetConfig returns an initialized Config
func (m *MockedManager) GetConfig() *rest.Config {
	return nil
}

// GetScheme returns and initialized Scheme
func (m *MockedManager) GetScheme() *runtime.Scheme {
	return nil
}

// GetAdmissionDecoder returns the runtime.Decoder based on the scheme.
func (m *MockedManager) GetAdmissionDecoder() admission.Decoder {
	// just returning nothing
	d, _ := admission.NewDecoder(runtime.NewScheme())
	return *d
}

// GetAPIReader returns the client reader
func (m *MockedManager) GetAPIReader() k8sClient.Reader {
	return nil
}

// GetClient returns a client configured with the Config
func (m *MockedManager) GetClient() k8sClient.Client {
	return m.Client
}

func (m *MockedManager) GetEventRecorderFor(_ string) record.EventRecorder {
	return nil
}

// GetFieldIndexer returns a client.FieldIndexer configured with the client
func (m *MockedManager) GetFieldIndexer() k8sClient.FieldIndexer {
	return nil
}

// GetCache returns a cache.Cache
func (m *MockedManager) GetCache() cache.Cache {
	return nil
}

// GetRecorder returns a new EventRecorder for the provided name
func (m *MockedManager) GetRecorder(_ string) record.EventRecorder {
	return nil
}

// GetRESTMapper returns a RESTMapper
func (m *MockedManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m *MockedManager) GetWebhookServer() *webhook.Server {
	return nil
}

func (m *MockedManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	return nil
}

// AddHealthzCheck allows you to add Healthz checker
func (m *MockedManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

// AddReadyzCheck allows you to add Readyz checker
func (m *MockedManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

func (m *MockedManager) GetLogger() logr.Logger {
	return nil
}

func (m *MockedManager) GetControllerOptions() v1alpha1.ControllerConfigurationSpec {
	var duration = time.Duration(0)
	return v1alpha1.ControllerConfigurationSpec{
		CacheSyncTimeout: &duration,
	}
}
