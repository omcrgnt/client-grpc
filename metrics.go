package clientgrpc

import (
	"github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/omcrgnt/res/unique"
	prom "github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

// GRPCMetrics is a singleton pool resource: contributor + shared prometheus
// interceptors for all client-grpc clients. RegisterMetrics matches ops metrics
// duck-typing (no ops import).
type GRPCMetrics struct {
	clientMetrics *prometheus.ClientMetrics
}

func (m *GRPCMetrics) RegisterMetrics(reg *prom.Registry) error {
	m.clientMetrics = prometheus.NewClientMetrics(prometheus.WithClientHandlingTimeHistogram())
	reg.MustRegister(m.clientMetrics)
	return nil
}

func (m *GRPCMetrics) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return m.clientMetrics.UnaryClientInterceptor()
}

func (m *GRPCMetrics) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return m.clientMetrics.StreamClientInterceptor()
}

func init() {
	unique.MustAddFixed(&GRPCMetrics{})
}
