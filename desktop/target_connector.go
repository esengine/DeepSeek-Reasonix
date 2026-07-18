package main

import (
	"context"
	"errors"
)

// TargetConnectorMux keeps TargetManager target-neutral while giving Local and
// Remote lifecycle adapters separate construction dependencies. It never
// substitutes one target when the selected connector fails.
type TargetConnectorMux struct {
	Local  TargetConnector
	Remote TargetConnector
}

func (m TargetConnectorMux) Connect(ctx context.Context, target TargetDescriptor) (TargetAdapter, error) {
	switch target.Kind {
	case TargetLocal:
		if m.Local == nil {
			return nil, errors.New("Local target connector is unavailable")
		}
		return m.Local.Connect(ctx, target)
	case TargetRemote:
		if m.Remote == nil {
			return nil, errors.New("Remote target connector is unavailable")
		}
		return m.Remote.Connect(ctx, target)
	default:
		return nil, target.Validate()
	}
}

func (m TargetConnectorMux) Reconnect(ctx context.Context, target TargetDescriptor, recovery TargetAdapter) (TargetAdapter, error) {
	if target.Kind != TargetRemote {
		return nil, ErrTargetReconnectUnsupported
	}
	reconnector, ok := m.Remote.(TargetReconnector)
	if !ok || reconnector == nil {
		return nil, ErrTargetReconnectUnsupported
	}
	return reconnector.Reconnect(ctx, target, recovery)
}

var _ TargetConnector = TargetConnectorMux{}
var _ TargetReconnector = TargetConnectorMux{}
