package balancer

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"gateway-service/internal/config"
)

type instance struct {
	addr        string
	activeConns int64
	healthy     int32
}

func (i *instance) isHealthy() bool { return atomic.LoadInt32(&i.healthy) == 1 }

func (i *instance) setHealthy(v bool) {
	if v {
		atomic.StoreInt32(&i.healthy, 1)
	} else {
		atomic.StoreInt32(&i.healthy, 0)
	}
}

func (i *instance) Done() { atomic.AddInt64(&i.activeConns, -1) }

func (i *instance) Addr() string { return i.addr }

type LeastConn struct {
	instances []*instance
}

func NewLeastConn(addrs []string) (*LeastConn, error) {
	if len(addrs) == 0 {
		return nil, errors.New("balancer: at least one address is required")
	}
	instances := make([]*instance, len(addrs))
	for idx, a := range addrs {
		instances[idx] = &instance{addr: a}
		instances[idx].setHealthy(true)
	}
	return &LeastConn{instances: instances}, nil
}

func NewLeastConnWithConfig(addrs []string, cfg config.HealthCheckConfig, log *slog.Logger) (*LeastConn, *HealthChecker, error) {
	lc, err := NewLeastConn(addrs)
	if err != nil {
		return nil, nil, fmt.Errorf("NewLeastConn: %w", err)
	}
	hc := NewHealthChecker(lc.instances, cfg, log)
	return lc, hc, nil
}

func (lc *LeastConn) Next() *instance {
	var best *instance
	for _, inst := range lc.instances {
		if !inst.isHealthy() {
			continue
		}
		if best == nil || atomic.LoadInt64(&inst.activeConns) < atomic.LoadInt64(&best.activeConns) {
			best = inst
		}
	}
	if best != nil {
		atomic.AddInt64(&best.activeConns, 1)
	}
	return best
}

func (lc *LeastConn) Len() int { return len(lc.instances) }
