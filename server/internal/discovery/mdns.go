package discovery

import "github.com/grandcat/zeroconf"

// ServiceType is the mDNS service type advertised on the local network.
// Android clients use NsdManager to discover services of this type.
const ServiceType = "_androidmac._tcp"

// Service manages the mDNS advertisement for the server.
type Service struct {
	server *zeroconf.Server
}

// NewService registers a new mDNS service on the local network with the given
// instance name and port. The service is discoverable immediately after creation.
func NewService(instanceName string, port int) (*Service, error) {
	server, err := zeroconf.Register(
		instanceName,
		ServiceType,
		"local.",
		port,
		[]string{"version=1.0"},
		nil,
	)
	if err != nil {
		return nil, err
	}
	return &Service{server: server}, nil
}

// Stop shuts down the mDNS advertisement gracefully.
func (s *Service) Stop() {
	if s.server != nil {
		s.server.Shutdown()
	}
}
