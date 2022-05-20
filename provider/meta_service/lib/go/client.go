package metaservice

import (
	"google.golang.org/grpc"
)

func NewLBDial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`))
	return grpc.Dial(target, opts...)
}
