package server

import (
	"context"
	"errors"

	"github.com/oldsj/opencozy-next/internal/api"
)

// Slice 7 (devices): the contract is published first so the kid app can be built against it.
// These answer 500 until the handlers land in this PR.
var errNotYet = errors.New("devices: not implemented yet")

// CreateDevicePairing is slice 7.
func (s *Server) CreateDevicePairing(_ context.Context, _ api.CreateDevicePairingRequestObject) (api.CreateDevicePairingResponseObject, error) {
	return nil, errNotYet
}

// CreateDeviceWebSession is slice 7.
func (s *Server) CreateDeviceWebSession(_ context.Context, _ api.CreateDeviceWebSessionRequestObject) (api.CreateDeviceWebSessionResponseObject, error) {
	return nil, errNotYet
}

// GetDevice is slice 7.
func (s *Server) GetDevice(_ context.Context, _ api.GetDeviceRequestObject) (api.GetDeviceResponseObject, error) {
	return nil, errNotYet
}

// GetDeviceWebSession is slice 7.
func (s *Server) GetDeviceWebSession(_ context.Context, _ api.GetDeviceWebSessionRequestObject) (api.GetDeviceWebSessionResponseObject, error) {
	return nil, errNotYet
}

// ListDevices is slice 7.
func (s *Server) ListDevices(_ context.Context, _ api.ListDevicesRequestObject) (api.ListDevicesResponseObject, error) {
	return nil, errNotYet
}

// PairDevice is slice 7.
func (s *Server) PairDevice(_ context.Context, _ api.PairDeviceRequestObject) (api.PairDeviceResponseObject, error) {
	return nil, errNotYet
}

// RevokeDevice is slice 7.
func (s *Server) RevokeDevice(_ context.Context, _ api.RevokeDeviceRequestObject) (api.RevokeDeviceResponseObject, error) {
	return nil, errNotYet
}

// SetLockMessage is slice 7.
func (s *Server) SetLockMessage(_ context.Context, _ api.SetLockMessageRequestObject) (api.SetLockMessageResponseObject, error) {
	return nil, errNotYet
}

// UpdateDevice is slice 7.
func (s *Server) UpdateDevice(_ context.Context, _ api.UpdateDeviceRequestObject) (api.UpdateDeviceResponseObject, error) {
	return nil, errNotYet
}
