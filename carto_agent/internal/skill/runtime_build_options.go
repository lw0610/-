package skill

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource"

	"github.com/autocarto/carto_agent/internal/compute"
)

type ArgsMutator func(context.Context, map[string]any)

type QGISInvoker func(context.Context, string, map[string]any) (any, error)

type QGISResponseInvoker func(context.Context, string, map[string]any) (*compute.InvokeResponse, error)

type QGISRuntimeInspector interface {
	ProviderID() string
	ActiveInstance() string
	HasInstanceConnected(instanceID string) bool
	ConnectedInstanceIDs() []string
}

type AutoLoadFunc func(context.Context, map[string]any, []compute.OutputLayer)

type InternalInvoker func(context.Context, string, map[string]any) (any, error)

type BuildOptions struct {
	ResourceResolver     resource.Resolver
	ComputeClient        *compute.Client
	QGISInvoker          QGISInvoker
	QGISResponseInvoker  QGISResponseInvoker
	QGISRuntimeInspector QGISRuntimeInspector
	InternalInvoker      InternalInvoker
	ArgsMutator          ArgsMutator
	AutoLoad             AutoLoadFunc
}
