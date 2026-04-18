package balancer

import (
	"sync"
	"sync/atomic"
	"testing"
)

func newPool(addrs ...string) *LeastConn {
	lc, err := NewLeastConn(addrs)
	if err != nil {
		panic(err)
	}
	return lc
}

func TestNext_PicksLeastLoaded(t *testing.T) {
	lc := newPool("a:80", "b:80", "c:80")

	atomic.StoreInt64(&lc.instances[1].activeConns, 5)

	atomic.StoreInt64(&lc.instances[2].activeConns, 10)

	inst := lc.Next()
	if inst == nil {
		t.Fatal("expected an instance, got nil")
	}
	if inst.addr != "a:80" {
		t.Errorf("expected a:80 (least loaded), got %s", inst.addr)
	}
	inst.Done()
}

func TestNext_DoneDecrementsCounter(t *testing.T) {
	lc := newPool("a:80")

	inst := lc.Next()
	if inst == nil {
		t.Fatal("unexpected nil")
	}
	if atomic.LoadInt64(&inst.activeConns) != 1 {
		t.Errorf("activeConns should be 1 after Next(), got %d", atomic.LoadInt64(&inst.activeConns))
	}

	inst.Done()
	if atomic.LoadInt64(&inst.activeConns) != 0 {
		t.Errorf("activeConns should be 0 after Done(), got %d", atomic.LoadInt64(&inst.activeConns))
	}
}

func TestNext_EqualLoadPicksFirst(t *testing.T) {
	lc := newPool("first:80", "second:80")
	inst := lc.Next()
	if inst == nil || inst.addr != "first:80" {
		t.Errorf("expected first:80, got %v", inst)
	}
	inst.Done()
}

func TestNext_EmptyPool(t *testing.T) {
	_, err := NewLeastConn(nil)
	if err == nil {
		t.Error("expected error for empty addr list")
	}
}

func TestNext_SkipsUnhealthy(t *testing.T) {
	lc := newPool("a:80", "b:80")
	lc.instances[0].setHealthy(false)

	inst := lc.Next()
	if inst == nil {
		t.Fatal("expected b:80, got nil")
	}
	if inst.addr != "b:80" {
		t.Errorf("expected b:80, got %s", inst.addr)
	}
	inst.Done()
}

func TestNext_AllUnhealthyReturnsNil(t *testing.T) {
	lc := newPool("a:80", "b:80")
	for _, i := range lc.instances {
		i.setHealthy(false)
	}

	inst := lc.Next()
	if inst != nil {
		t.Errorf("expected nil when all unhealthy, got %s", inst.addr)
	}
}

func TestNext_RecoveredInstanceRejoinsPool(t *testing.T) {
	lc := newPool("a:80")
	lc.instances[0].setHealthy(false)

	if lc.Next() != nil {
		t.Error("should return nil while unhealthy")
	}

	lc.instances[0].setHealthy(true)
	inst := lc.Next()
	if inst == nil || inst.addr != "a:80" {
		t.Error("recovered instance should be selected")
	}
	inst.Done()
}

func TestNext_ConcurrentSafety(t *testing.T) {
	lc := newPool("a:80", "b:80", "c:80")

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst := lc.Next()
			if inst != nil {
				inst.Done()
			}
		}()
	}
	wg.Wait()

	for _, inst := range lc.instances {
		if v := atomic.LoadInt64(&inst.activeConns); v != 0 {
			t.Errorf("addr=%s leaked activeConns=%d", inst.addr, v)
		}
	}
}
