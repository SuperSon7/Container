package ipam

import (
	"fmt"
	"sync"
	"testing"
)

func TestAllocateExhaustReleaseAndReuse(t *testing.T) {
	a, err := New("10.0.0.1/30")
	if err != nil {
		t.Fatal(err)
	}
	ip, err := a.Allocate("a")
	if err != nil || ip != "10.0.0.2/30" {
		t.Fatalf("got %s %v", ip, err)
	}
	again, err := a.Allocate("a")
	if err != nil || again != ip {
		t.Fatalf("same owner: %s %v", again, err)
	}
	if _, err := a.Allocate("b"); err == nil {
		t.Fatal("allocated gateway, network or broadcast")
	}
	if err := a.Release("a"); err != nil {
		t.Fatal(err)
	}
	if err := a.Release("a"); err != nil {
		t.Fatal(err)
	}
	ip, err = a.Allocate("b")
	if err != nil || ip != "10.0.0.2/30" {
		t.Fatalf("reuse: %s %v", ip, err)
	}
}

func TestConcurrentAllocationsAreUnique(t *testing.T) {
	a, err := New("10.0.0.1/24")
	if err != nil {
		t.Fatal(err)
	}
	const count = 100
	addresses := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ip, err := a.Allocate(fmt.Sprint(i))
			if err != nil {
				t.Error(err)
				return
			}
			addresses <- ip
		}(i)
	}
	wg.Wait()
	close(addresses)
	seen := map[string]bool{}
	for ip := range addresses {
		if seen[ip] {
			t.Fatalf("duplicate %s", ip)
		}
		seen[ip] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d leases", len(seen))
	}
}

func TestRejectInvalidPoolsAndEmptyOwner(t *testing.T) {
	for _, cidr := range []string{"bad", "10.0.0.1", "10.0.0.0/24", "10.0.0.255/24", "10.0.0.1/31", "::1/64"} {
		if _, err := New(cidr); err == nil {
			t.Fatalf("accepted %s", cidr)
		}
	}
	a, err := New("10.0.0.1/24")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate(""); err == nil {
		t.Fatal("accepted empty ID")
	}
}
