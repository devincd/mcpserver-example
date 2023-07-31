package bootstrap

import (
	mcpkeepalive "devincd.io/mcpserver-example/pkg/keepalive"
)

// MCPServerArgs 启动参数结构体
type MCPServerArgs struct {
	GrpcPort         string
	HTTPPort         string
	KeepaliveOptions *mcpkeepalive.Options
	Kubeconfig       string
}

func NewMCPServerArgs() *MCPServerArgs {
	return &MCPServerArgs{
		KeepaliveOptions: mcpkeepalive.DefaultOption(),
	}
}
