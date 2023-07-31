package grpc

import (
	"context"
	"io"
	"strings"
	"time"

	"devincd.io/mcpserver-example/pkg/features"
	mcpkeepalive "devincd.io/mcpserver-example/pkg/keepalive"
	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

type SendHandler func() error

var xdsPushSendTimeout = 0 * time.Second

// Send with timeout if specified. If timeout is zero, sends without timeout.
func Send(ctx context.Context, send SendHandler) error {
	if xdsPushSendTimeout.Nanoseconds() > 0 {
		errChan := make(chan error, 1)
		timeoutCtx, cancel := context.WithTimeout(ctx, xdsPushSendTimeout)
		defer cancel()
		go func() {
			err := send()
			errChan <- err
			close(errChan)
		}()
		select {
		case <-timeoutCtx.Done():
			return status.Errorf(codes.DeadlineExceeded, "timeout sending")
		case err := <-errChan:
			return err
		}
	}
	err := send()
	return err
}

func ServerOptions(options *mcpkeepalive.Options, interceptors ...grpc.UnaryServerInterceptor) []grpc.ServerOption {
	maxStreams := features.MaxConcurrentStreams
	maxRecvMsgSize := features.MaxRecvMsgSize

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.MaxRecvMsgSize(maxRecvMsgSize),
		// Ensure we allow clients sufficient ability to send keep alives. If this is higher than client
		// keep alive setting, it will prematurely get a GOAWAY sent.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: options.Time / 2,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

var expectedGrpcFailureMessages = []string{
	"client disconnected",
	"error reading from server: EOF",
	"transport is closing",
}

func containsExpectedMessage(msg string) bool {
	for _, m := range expectedGrpcFailureMessages {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// IsExpectedGRPCError checks a gRPC error code and determines whether it is an expected error when
// things are operating normally. This is basically capturing when the client disconnects.
func IsExpectedGRPCError(err error) bool {
	if err == io.EOF {
		return true
	}

	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.Canceled || s.Code() == codes.DeadlineExceeded {
			return true
		}
		if s.Code() == codes.Unavailable && containsExpectedMessage(s.Message()) {
			return true
		}
	}
	// If this is not a gRPCStatus we should just error message.
	if strings.Contains(err.Error(), "stream terminated by RST_STREAM with error code: NO_ERROR") {
		return true
	}

	return false
}
