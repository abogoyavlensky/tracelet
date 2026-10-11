package otlp

import commonpb "go.opentelemetry.io/proto/otlp/common/v1"

// UnknownService is the service of records whose resource has no
// service.name, as the OpenTelemetry SDKs default it.
const UnknownService = "unknown_service"

// DefaultEnvironment is the environment of records whose resource has no
// deployment.environment.name.
const DefaultEnvironment = "default"

// Resource attributes Tracelet files rows under.
const (
	attrServiceName    = "service.name"
	attrServiceVersion = "service.version"
	attrEnvironment    = "deployment.environment.name"
)

// resourceInfo reads what every row takes from its resource: the service
// (UnknownService when absent, with unknown set), the environment
// (DefaultEnvironment when absent), the version, and the resource attributes
// as JSON.
func resourceInfo(attrs []*commonpb.KeyValue) (service, environment, version, resourceJSON string, unknown bool) {
	service = stringAttr(attrs, attrServiceName)
	unknown = service == ""
	if unknown {
		service = UnknownService
	}
	environment = stringAttr(attrs, attrEnvironment)
	if environment == "" {
		environment = DefaultEnvironment
	}
	return service, environment, stringAttr(attrs, attrServiceVersion), jsonObject(attrs), unknown
}
