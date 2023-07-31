package bootstrap

import (
	"net/http"

	"github.com/gorilla/mux"
)

func registerRouter(router *mux.Router, mcpserver *MCPServer) {
	router.HandleFunc("/health", mcpserver.healthCheck).Methods("GET")
}

func (s *MCPServer) healthCheck(write http.ResponseWriter, req *http.Request) {
	// 检查xds grpc server是否正常
	// TODO 改成检查对grpc服务的探查
	if s.IsServerReady() {
		write.WriteHeader(http.StatusOK)
	} else {
		write.WriteHeader(http.StatusBadGateway)
	}
}
