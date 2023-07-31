package bootstrap

import (
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Connection holds information about connected client.
type Connection struct {
	// PeerAddr is the address of the client, from network layer.
	PeerAddr string
	// Time of connection, for debugging
	Connect time.Time
	// ConID is the connection identifier, used as a key in the connection table.
	// Current based on the node name and a counter.
	ConID string
	// Sending on this channel results in a push.
	pushChannel chan *Event
	// Both ADS and SDS streams implement this interface
	stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer
	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node
	// reqChan is used to receive discovery requests for this connection.
	reqChan chan *discovery.DiscoveryRequest
	// errorChan is used to process error during discovery request processing.
	errorChan chan error
	// WatchedResources contains the list of watched resources for the connection, keyed by the DiscoveryRequest TypeUrl.
	watchedResources      map[string]*WatchedResource
	watchedResourcesMutex sync.RWMutex
}

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string
	// VersionSent is the version of the resource included in the last sent response.
	// It corresponds to the [Cluster/Route/Listener]VersionSent in the XDS package.
	VersionSent string
	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string
	// VersionAcked represents the version that was applied successfully. It can be different from
	// VersionSent: if NonceSent == NonceAcked and versions are different it means the client rejected
	// the last version, and VersionAcked is the last accepted and active config.
	// If empty it means the client has no accepted/valid version, and is not ready.
	VersionAcked string
	// NonceAcked is the last acked message.
	NonceAcked string
	// NonceNacked is the last nacked message. This is reset following a successful ACK
	NonceNacked string
	// LastSent tracks the time of the generated push, to determine the time it takes the client to ack.
	LastSent time.Time
	// Updates count the number of generated updates for the resource
	Updates int
	// LastSize tracks the size of the last update
	LastSize int
	// Last request contains the last DiscoveryRequest received for
	// this type. Generators are called immediately after each request,
	// and may use the information in DiscoveryRequest.
	// Note that Envoy may send multiple requests for the same type, for
	// example to update the set of watched resources or to ACK/NACK.
	LastRequest *discovery.DiscoveryRequest
}

// Event represents a config or registry event that results in a push.
type Event struct {
	//  pushRequest to use for the push.
	pushRequest *PushRequest
	// function to call once a push is finished. This must be called or future changes may be blocked.
	done func()
}

type PushRequest struct {
	GVK string
}
