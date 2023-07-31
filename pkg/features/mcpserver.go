package features

// TODO 通过环境变量的方式暴露出去，可供配置
var (
	// MaxConcurrentStreams Sets the maximum number of concurrent grpc streams.
	MaxConcurrentStreams = 100000
	// MaxRecvMsgSize Sets the max receive buffer size of gRPC stream in bytes.
	MaxRecvMsgSize = 4 * 1024 * 1024
)
