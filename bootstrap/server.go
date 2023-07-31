package bootstrap

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"devincd.io/mcpserver-example/controller"
	mcpgrpc "devincd.io/mcpserver-example/pkg/grpc"
	mcpkeepalive "devincd.io/mcpserver-example/pkg/keepalive"
	"devincd.io/mcpserver-example/pkg/utils"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/gorilla/mux"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	"istio.io/istio/pkg/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	TerminationGracePeriod = 10 * time.Second
)

type MCPServer struct {
	args                 *MCPServerArgs
	kubeClientset        kubernetes.Interface
	istioClientset       istioclient.Interface
	istioInformerFactory istioinformers.SharedInformerFactory
	istioController      *controller.Controller
	grpcServer           *grpc.Server
	httpServer           *http.Server
	// 所有连接mcpserver的客户端
	adsClients      map[string]*Connection
	adsClientsMutex *sync.RWMutex
	// 需要监听的所有资源列表
	watchResources []config.GroupVersionKind
	// serverReady indicates caches have been synced up and server is ready to process requests.
	serverReady atomic.Bool
}

// NewMCPServer 初始化mcp服务器
func NewMCPServer(args *MCPServerArgs) (*MCPServer, error) {
	s := &MCPServer{
		args:            args,
		adsClients:      make(map[string]*Connection),
		adsClientsMutex: &sync.RWMutex{},
	}
	if err := s.initKubeClients(); err != nil {
		return s, err
	}
	if err := s.initIstioClients(); err != nil {
		return s, err
	}
	if err := s.initController(); err != nil {
		return s, err
	}
	if err := s.initGrpcServer(mcpkeepalive.DefaultOption()); err != nil {
		return s, err
	}
	s.initHttpServer()
	return s, nil
}

func (s *MCPServer) initKubeClients() error {
	kc, err := utils.GetKubeRestConfig("")
	if err != nil {
		return err
	}
	kubeClientset, err := kubernetes.NewForConfig(kc)
	if err != nil {
		return err
	}
	s.kubeClientset = kubeClientset
	return nil
}
func (s *MCPServer) initIstioClients() error {
	kc, err := utils.GetKubeRestConfig("")
	if err != nil {
		return err
	}
	// create istio client
	istioClientset, err := istioclient.NewForConfig(kc)
	if err != nil {
		return err
	}
	s.istioClientset = istioClientset
	return nil
}

func (s *MCPServer) initController() error {
	// WithNamespace limits the SharedInformerFactory to the specified namespace.
	istioInformerFactory := istioinformers.NewSharedInformerFactoryWithOptions(s.istioClientset, 0)
	s.istioInformerFactory = istioInformerFactory
	c, err := controller.NewIstioController(s.istioClientset, s.istioInformerFactory)
	if err != nil {
		return err
	}
	s.istioController = c
	return nil
}

func (s *MCPServer) initGrpcServer(options *mcpkeepalive.Options) error {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}
	grpcOptions := mcpgrpc.ServerOptions(options, interceptors...)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	// 注册grpc服务
	discovery.RegisterAggregatedDiscoveryServiceServer(s.grpcServer, s)
	return nil
}

func (s *MCPServer) initHttpServer() {
	router := mux.NewRouter()
	registerRouter(router, s)
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%s", s.args.HTTPPort),
		Handler:      router,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	s.httpServer = httpServer
}

func (s *MCPServer) Start(stop chan struct{}) error {
	klog.Info("Starting the mcp-over-xds server")
	// (1)启动controller逻辑
	go func() {
		s.istioInformerFactory.Start(stop)
		err := s.istioController.Run()
		if err != nil {
			klog.Fatalf("failed to run the istio controller: %v", err)
		}
	}()
	klog.Info("Started the istio controller")
	if ok := s.istioController.WaitForCacheSync(stop); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}
	// (2)启动grpc服务
	go func() {
		lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", s.args.GrpcPort))
		if err != nil {
			log.Fatalf("net.Listen err: %v", err)
		}
		err = s.grpcServer.Serve(lis)
		if err != nil {
			klog.Fatalf("failed to serve the grpc service: %v", err)
		}
	}()
	s.CachesSynced()
	klog.Info("Started the mcp grpc server")
	// (3)启动http服务
	go func() {
		err := s.httpServer.ListenAndServe()
		if err != nil {
			klog.Fatalf("failed to serve the http service: %v", err)
		}
		klog.Info("Started the http server")
	}()
	// (4)启动消费controller的逻辑
	go func() {
		err := s.RunIstioController(stop)
		if err != nil {
			klog.Fatalf("failed to run the consume istio controller: %v", err)
		}
	}()
	klog.Info("Started the consume istio controller")
	return nil
}

// GracefulShutdown 平滑终止服务
func (s *MCPServer) GracefulShutdown() {
	wg := &sync.WaitGroup{}
	if s.grpcServer != nil {
		// (1)平滑终止grpc服务
		go func() {
			wg.Add(1)
			defer wg.Done()
			stopped := make(chan struct{})
			go func() {
				s.grpcServer.GracefulStop()
				close(stopped)
			}()
			select {
			case <-time.After(TerminationGracePeriod):
				klog.Info("timeout!! and the grpcServer is force shutdown")
				s.grpcServer.Stop()
			case <-stopped:
				klog.Info("the grpcServer is gracefully shutdown")
			}
		}()
	}
	if s.httpServer != nil {
		// (2)平滑终止http服务
		go func() {
			wg.Add(1)
			defer wg.Done()
			timeContext, cancel := context.WithTimeout(context.Background(), TerminationGracePeriod)
			defer cancel()
			s.httpServer.Shutdown(timeContext)
			klog.Info("the httpServer is shutdown")
		}()
	}
	wg.Wait()
}
