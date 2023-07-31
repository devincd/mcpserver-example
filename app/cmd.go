package app

import (
	"flag"
	"fmt"

	"devincd.io/mcpserver-example/bootstrap"
	"devincd.io/mcpserver-example/pkg/cmd"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	mcpserverArgs *bootstrap.MCPServerArgs
)

func NewMCPServerCommand() *cobra.Command {
	rootCommand := &cobra.Command{
		Use:   "mcpserver",
		Short: "mcp-over-xds server",
		Args:  cobra.ExactArgs(0),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateArgs(); err != nil {
				return err
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			stop := make(chan struct{})
			// 初始化mcp服务
			mcpserver, err := bootstrap.NewMCPServer(mcpserverArgs)
			if err != nil {
				return fmt.Errorf("failed to create mcp-over-xds service: %v", err)
			}
			// 启动mcp服务
			if err := mcpserver.Start(stop); err != nil {
				return fmt.Errorf("failed to start mcp-over-xds service: %v", err)
			}
			cmd.WaitSignal(stop)
			mcpserver.GracefulShutdown()
			return nil
		},
	}
	addFlags(rootCommand)
	rootCommand.AddCommand(cmd.VersionCommand())
	return rootCommand
}

func addFlags(c *cobra.Command) {
	mcpserverArgs = bootstrap.NewMCPServerArgs()
	// add klog flag to the command
	klog.InitFlags(flag.CommandLine)
	cmd.AddFlags(c)

	c.PersistentFlags().StringVar(&mcpserverArgs.GrpcPort, "grpc-port", "15010", "grpc xds port")
	c.PersistentFlags().StringVar(&mcpserverArgs.HTTPPort, "http-port", "9080", "http port")
	c.PersistentFlags().StringVar(&mcpserverArgs.Kubeconfig, "kubeconfig", "", "")

	mcpserverArgs.KeepaliveOptions.AttachCobraFlags(c)
}

func validateArgs() error {
	return nil
}
