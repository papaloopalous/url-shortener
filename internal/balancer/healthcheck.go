package balancer

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gateway-service/internal/config"
	"gateway-service/pkg/metrics"
)

type HealthChecker struct {
	instances []*instance
	interval  time.Duration
	timeout   time.Duration
	client    *http.Client
	log       *slog.Logger
	upstream  string
}

func NewHealthChecker(instances []*instance, cfg config.HealthCheckConfig, log *slog.Logger) *HealthChecker {
	return &HealthChecker{
		instances: instances,
		interval:  cfg.Interval,
		timeout:   cfg.Timeout,
		client:    &http.Client{Timeout: cfg.Timeout},
		log:       log,
	}
}

func (hc *HealthChecker) WithUpstream(name string) *HealthChecker {
	hc.upstream = name
	return hc
}

func (hc *HealthChecker) Run(ctx context.Context) {
	hc.checkAll(ctx)

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.checkAll(ctx)
		}
	}
}

func (hc *HealthChecker) checkAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, inst := range hc.instances {
		wg.Add(1)
		go func(i *instance) {
			defer wg.Done()
			hc.check(ctx, i)
		}(inst)
	}
	wg.Wait()
}

func (hc *HealthChecker) check(ctx context.Context, inst *instance) {
	url := fmt.Sprintf("%s/healthz", inst.addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		hc.transition(inst, false, fmt.Sprintf("build request: %v", err))
		return
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		hc.transition(inst, false, err.Error())
		return
	}
	resp.Body.Close() //nolint:errcheck

	hc.transition(inst, resp.StatusCode == http.StatusOK, fmt.Sprintf("status %d", resp.StatusCode))
}

func (hc *HealthChecker) transition(inst *instance, newHealthy bool, reason string) {
	wasHealthy := inst.isHealthy()
	inst.setHealthy(newHealthy)

	metrics.SetInstanceHealth(hc.upstream, inst.addr, newHealthy)

	if wasHealthy == newHealthy {
		return
	}

	attrs := []any{
		"upstream", hc.upstream,
		"addr", inst.addr,
		"reason", reason,
	}
	if newHealthy {
		hc.log.Info("instance recovered", attrs...)
	} else {
		hc.log.Warn("instance unhealthy", attrs...)
	}
}
