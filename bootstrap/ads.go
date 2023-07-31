package bootstrap

import (
	"fmt"
	"istio.io/istio/pilot/pkg/grpc"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	mcpgrpc "devincd.io/mcpserver-example/pkg/grpc"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	mcp "istio.io/api/mcp/v1alpha1"
	v1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	v1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	securityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	telemetryv1alpha1 "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

var (
	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
	ResourceNumber   = int64(0)
)

// Send with timeout if configured
func (conn *Connection) send(res *discovery.DiscoveryResponse) error {
	sendHandler := func() error {
		return conn.stream.Send(res)
	}

	err := mcpgrpc.Send(conn.stream.Context(), sendHandler)
	if err == nil {
		sz := 0
		for _, rc := range res.Resources {
			sz += len(rc.Value)
		}
		if res.Nonce != "" {
			conn.watchedResourcesMutex.Lock()
			defer conn.watchedResourcesMutex.Unlock()
			if conn.watchedResources[res.TypeUrl] == nil {
				conn.watchedResources[res.TypeUrl] = &WatchedResource{TypeUrl: res.TypeUrl}
			}
			conn.watchedResources[res.TypeUrl].NonceSent = res.Nonce
			conn.watchedResources[res.TypeUrl].VersionSent = res.VersionInfo
			conn.watchedResources[res.TypeUrl].LastSent = time.Now()
			conn.watchedResources[res.TypeUrl].LastSize = sz
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		klog.Infof("Timeout writing %s", conn.ConID)
	}
	return err
}

func (s *MCPServer) addConn(connID string, conn *Connection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.adsClients[connID] = conn
}

func (s *MCPServer) removeConn(connID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if _, exist := s.adsClients[connID]; !exist {
		klog.Errorf("ADS: Removing connection for non-existing node: %v", connID)
	} else {
		delete(s.adsClients, connID)
	}
}

func (s *MCPServer) closeConnection(conn *Connection) {
	if conn.ConID == "" {
		return
	}
	s.removeConn(conn.ConID)
}

func connectionID(nodeID string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return nodeID + "-" + strconv.FormatInt(id, 10)
}

func VersionID() string {
	id := atomic.AddInt64(&ResourceNumber, 1)
	return fmt.Sprintf("%s-%s", time.Now().String(), strconv.FormatInt(id, 10))
}

func (s *MCPServer) initConnection(node *core.Node, conn *Connection) {
	conn.ConID = connectionID(node.Id)
	conn.node = node
	s.addConn(conn.ConID, conn)
}

func newConnection(peerAddr string, stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) *Connection {
	return &Connection{
		pushChannel:      make(chan *Event),
		reqChan:          make(chan *discovery.DiscoveryRequest, 1),
		errorChan:        make(chan error, 1),
		PeerAddr:         peerAddr,
		Connect:          time.Now(),
		stream:           stream,
		watchedResources: make(map[string]*WatchedResource),
	}
}

func (s *MCPServer) receive(conn *Connection) {
	defer func() {
		close(conn.reqChan)
		s.closeConnection(conn)
	}()
	firstRequest := true
	for {
		req, err := conn.stream.Recv()
		if err != nil {
			if grpc.IsExpectedGRPCError(err) {
				klog.Infof("ADS: %q %s terminated %v", conn.PeerAddr, conn.ConID, err)
				return
			}
			conn.errorChan <- err
			klog.Errorf("ADS: %q %s terminated with error: %v", conn.PeerAddr, conn.ConID, err)
			return
		}
		if firstRequest {
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				conn.errorChan <- status.New(codes.InvalidArgument, "missing nde information").Err()
				return
			}
			s.initConnection(req.Node, conn)
			klog.Infof("ADS: new connection for node: %s", conn.ConID)
		}
		select {
		case conn.reqChan <- req:
		case <-conn.stream.Context().Done():
			klog.Infof("ADS: %s %s terminated with stream closed", conn.PeerAddr, conn.ConID)
			return
		}
	}
}

func (s *MCPServer) StreamAggregatedResources(stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	// 需要保证cache已经都同步完成了
	if !s.IsServerReady() {
		return status.Error(codes.Unavailable, "server is not ready to serve discovery information")
	}
	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}
	conn := newConnection(peerAddr, stream)
	go s.receive(conn)
	for {
		select {
		case req, ok := <-conn.reqChan:
			if ok {
				if err := s.processRequest(req, conn); err != nil {
					return err
				}
			} else {
				return <-conn.errorChan
			}
		case pushEv := <-conn.pushChannel:
			err := s.pushConnection(conn, pushEv)
			if err != nil {
				return err
			}
		}
	}
}

// processRequest is handling one request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *MCPServer) processRequest(req *discovery.DiscoveryRequest, conn *Connection) error {
	switch {
	// 接受ACK
	case req.VersionInfo != "":
		klog.InfoS("Received ACK", "connID", conn.ConID, "versionInfo", req.VersionInfo, "typeURL", req.TypeUrl)
		return nil
		// 接受NACK
	case req.VersionInfo == "" && req.ErrorDetail != nil:
		klog.InfoS("Received NACK", "connID", conn.ConID, "versionInfo", req.VersionInfo, "typeURL", req.TypeUrl,
			"errorDetail", req.ErrorDetail)
		return nil
	default:
		// 正常请求
		klog.InfoS("Received Request", "connID", conn.ConID, "versionInfo", req.VersionInfo, "typeURL", req.TypeUrl)
		gvk := strings.SplitN(req.TypeUrl, "/", 3)
		if len(gvk) != 3 {
			return nil
		}
		retList, err := s.istioController.List(req.TypeUrl)
		if err != nil {
			klog.ErrorS(err, "List the istio resources with a error", "connID", conn.ConID, "versionInfo", req.VersionInfo, "typeURL", req.TypeUrl)
			return nil
		}
		groupVersionKind := schema.GroupVersionKind{Group: gvk[0], Version: gvk[1], Kind: gvk[2]}
		res := &discovery.DiscoveryResponse{
			TypeUrl:     req.TypeUrl,
			Nonce:       VersionID(),
			VersionInfo: VersionID(),
		}
		for _, obj := range retList {
			r, err := constructResourceToAny(obj, groupVersionKind)
			if err != nil {
				klog.ErrorS(err, "constructResourceToAny with a error",
					"connID", conn.ConID, "versionInfo", req.VersionInfo, "typeURL", req.TypeUrl)
				return nil
			}
			res.Resources = append(res.Resources, r)
		}
		// 无论请求的资源是否有，都执行推送的操作
		err = conn.send(res)
		if err != nil {
			return err
		}
	}
	return nil
}

// constructResourceToAny 类型转换
// 将kubernetes中的runtime.Object类型转换成protobuf中的any.Any类型
func constructResourceToAny(obj runtime.Object, gvk schema.GroupVersionKind) (*any.Any, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	resource := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:        fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetName()),
			CreateTime:  timestamppb.New(accessor.GetCreationTimestamp().Time),
			Version:     accessor.GetResourceVersion(),
			Labels:      accessor.GetLabels(),
			Annotations: accessor.GetAnnotations(),
		},
	}
	body := &any.Any{}
	var errMarshal error
	switch gvk {
	// Group=extensions.istio.io, Version=v1alpha1
	case v1alpha1.SchemeGroupVersion.WithKind("WasmPlugin"):
		body, errMarshal = anypb.New(&obj.(*v1alpha1.WasmPlugin).Spec)

		// Group=networking.istio.io, Version=v1alpha3
	case v1alpha3.SchemeGroupVersion.WithKind("DestinationRule"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.DestinationRule).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("EnvoyFilter"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.EnvoyFilter).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("Gateway"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.Gateway).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("ServiceEntry"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.ServiceEntry).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("Sidecar"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.Sidecar).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("VirtualService"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.VirtualService).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("WorkloadEntry"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.WorkloadEntry).Spec)
	case v1alpha3.SchemeGroupVersion.WithKind("WorkloadGroup"):
		body, errMarshal = anypb.New(&obj.(*v1alpha3.WorkloadGroup).Spec)

		// Group=networking.istio.io, Version=v1beta1
	case v1beta1.SchemeGroupVersion.WithKind("DestinationRule"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.DestinationRule).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("Gateway"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.Gateway).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("ProxyConfig"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.ProxyConfig).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("ServiceEntry"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.ServiceEntry).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("Sidecar"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.Sidecar).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("VirtualService"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.VirtualService).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("WorkloadEntry"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.WorkloadEntry).Spec)
	case v1beta1.SchemeGroupVersion.WithKind("WorkloadGroup"):
		body, errMarshal = anypb.New(&obj.(*v1beta1.WorkloadGroup).Spec)

		// Group=security.istio.io, Version=v1beta1
	case securityv1beta1.SchemeGroupVersion.WithKind("AuthorizationPolicy"):
		body, errMarshal = anypb.New(&obj.(*securityv1beta1.AuthorizationPolicy).Spec)
	case securityv1beta1.SchemeGroupVersion.WithKind("PeerAuthentication"):
		body, errMarshal = anypb.New(&obj.(*securityv1beta1.PeerAuthentication).Spec)
	case securityv1beta1.SchemeGroupVersion.WithKind("RequestAuthentication"):
		body, errMarshal = anypb.New(&obj.(*securityv1beta1.RequestAuthentication).Spec)

		// Group=telemetry.istio.io, Version=v1alpha1
	case telemetryv1alpha1.SchemeGroupVersion.WithKind("Telemetry"):
		body, errMarshal = anypb.New(&obj.(*telemetryv1alpha1.Telemetry).Spec)
	default:
		return nil, fmt.Errorf("no istio type find: %s", gvk.String())
	}
	if errMarshal != nil {
		return nil, errMarshal
	}
	resource.Body = body
	resAny, _ := anypb.New(resource)
	return &any.Any{
		TypeUrl: resAny.TypeUrl,
		Value:   resAny.Value,
	}, nil
}

// Compute and send the new configuration for a connection.
func (s *MCPServer) pushConnection(conn *Connection, pushEv *Event) error {
	klog.InfoS("push Connection", "connID", conn.ConID, "typeURL", pushEv.pushRequest.GVK)
	retList, err := s.istioController.List(pushEv.pushRequest.GVK)
	if err != nil {
		klog.ErrorS(err, "List the istio resources with a error", "connID", conn.ConID, "typeURL", pushEv.pushRequest.GVK)
		return nil
	}
	gvk := strings.SplitN(pushEv.pushRequest.GVK, "/", 3)
	groupVersionKind := schema.GroupVersionKind{Group: gvk[0], Version: gvk[1], Kind: gvk[2]}
	res := &discovery.DiscoveryResponse{
		TypeUrl:     pushEv.pushRequest.GVK,
		Nonce:       VersionID(),
		VersionInfo: VersionID(),
	}
	for _, obj := range retList {
		r, err := constructResourceToAny(obj, groupVersionKind)
		if err != nil {
			klog.ErrorS(err, "constructResourceToAny with a error", "connID", conn.ConID, "typeURL", pushEv.pushRequest.GVK)
			return nil
		}
		res.Resources = append(res.Resources, r)
	}
	if len(res.Resources) > 0 {
		err = conn.send(res)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *MCPServer) RunIstioController(stopCh chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer s.istioController.Workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting Istio workqueue controller")
	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := s.istioController.WaitForCacheSync(stopCh); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch two workers to process Foo resources
	for i := 0; i < 1; i++ {
		go wait.Until(s.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")
	s.istioController.Workqueue.ShutDown()
	return nil
}

func (s *MCPServer) runWorker() {
	for s.processNextWorkItem() {
	}
}

func (s *MCPServer) processNextWorkItem() bool {
	obj, shutdown := s.istioController.Workqueue.Get()
	if shutdown {
		return false
	}
	defer s.istioController.Workqueue.Done(obj)
	var key string
	var ok bool
	if key, ok = obj.(string); !ok {
		// As the item in the workqueue is actually invalid, we call
		// Forget here else we'd go into a loop of attempting to
		// process a work item that is invalid.
		s.istioController.Workqueue.Forget(obj)
		klog.Error(fmt.Errorf("expected string in workqueue but got %#v", obj))
		return true
	}
	for _, ads := range s.adsClients {
		for typeUrl, _ := range ads.watchedResources {
			if typeUrl == key {
				event := &Event{
					pushRequest: &PushRequest{
						GVK: key,
					},
				}
				ads.pushChannel <- event
			}
		}
	}
	s.istioController.Workqueue.Forget(obj)
	klog.Infof("Successfully synced '%s'", key)
	return true
}

func (s *MCPServer) DeltaAggregatedResources(delta discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return nil
}

var processStartTime = time.Now()

// CachesSynced is called when caches have been synced so that server can accept connections.
func (s *MCPServer) CachesSynced() {
	klog.Infof("All caches have been synced up in %v, marking server ready", time.Since(processStartTime))
	s.serverReady.Store(true)
}

func (s *MCPServer) IsServerReady() bool {
	return s.serverReady.Load()
}

func (s *MCPServer) addWatchResources() {

}
