package main

import (
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
)

func TestWaitForParentSyncReturnsWhenEnvMissing(t *testing.T) {
	t.Setenv(childSyncEnv, "")

	if err := waitForParentSync(); err != nil {
		t.Fatalf("expected no sync wait without env, got %v", err)
	}
}

func TestWaitForParentSyncRejectsInvalidFD(t *testing.T) {
	t.Setenv(childSyncEnv, "not-a-fd")

	err := waitForParentSync()
	if err == nil {
		t.Fatal("expected invalid fd error")
	}
	if !strings.Contains(err.Error(), "parse child sync fd") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestWaitForParentSyncBlocksUntilRelease(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()
	defer writePipe.Close()

	syncFD, err := duplicateFD(readPipe)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(childSyncEnv, strconv.Itoa(syncFD))

	done := make(chan error, 1)
	go func() {
		done <- waitForParentSync()
	}()

	select {
	case err := <-done:
		t.Fatalf("expected child sync to block before release, got %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	if _, err := writePipe.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected release to unblock child sync, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for child sync release")
	}
}

func TestWaitForParentSyncReportsClosedPipe(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()

	syncFD, err := duplicateFD(readPipe)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(childSyncEnv, strconv.Itoa(syncFD))

	if err := writePipe.Close(); err != nil {
		t.Fatal(err)
	}

	err = waitForParentSync()
	if err == nil {
		t.Fatal("expected closed pipe error")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReleaseChildWritesSignalAndClosesPipe(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()

	if err := releaseChild(writePipe); err != nil {
		t.Fatal(err)
	}

	var signal [1]byte
	if _, err := io.ReadFull(readPipe, signal[:]); err != nil {
		t.Fatalf("expected release signal, got %v", err)
	}
	if signal[0] != 1 {
		t.Fatalf("expected release byte 1, got %d", signal[0])
	}

	var extra [1]byte
	if _, err := readPipe.Read(extra[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("expected pipe close after release, got %v", err)
	}
}

func TestHostDefaultOutboundInterfaceUsesDefaultRoute(t *testing.T) {
	_, nonDefaultDst, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	useRouteList(t, []netlink.Route{
		{Dst: nonDefaultDst, LinkIndex: 3},
		{Dst: nil, LinkIndex: 7},
	}, nil)
	useLinkByIndex(t, func(index int) (netlink.Link, error) {
		if index != 7 {
			t.Fatalf("expected link index 7, got %d", index)
		}

		attrs := netlink.NewLinkAttrs()
		attrs.Name = "ens3"
		return &netlink.Dummy{LinkAttrs: attrs}, nil
	})

	got, err := hostDefaultOutboundInterface()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "ens3" {
		t.Fatalf("expected outbound interface ens3, got %s", got)
	}
}

func TestHostDefaultOutboundInterfaceAcceptsZeroCIDRDefaultRoute(t *testing.T) {
	_, defaultDst, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		t.Fatal(err)
	}
	useRouteList(t, []netlink.Route{{Dst: defaultDst, LinkIndex: 2}}, nil)
	useLinkByIndex(t, func(index int) (netlink.Link, error) {
		if index != 2 {
			t.Fatalf("expected link index 2, got %d", index)
		}

		attrs := netlink.NewLinkAttrs()
		attrs.Name = "eth0"
		return &netlink.Dummy{LinkAttrs: attrs}, nil
	})

	got, err := hostDefaultOutboundInterface()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "eth0" {
		t.Fatalf("expected outbound interface eth0, got %s", got)
	}
}

func TestHostDefaultOutboundInterfaceRejectsMissingDefaultRoute(t *testing.T) {
	_, nonDefaultDst, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	useRouteList(t, []netlink.Route{{Dst: nonDefaultDst, LinkIndex: 3}}, nil)
	useLinkByIndex(t, func(index int) (netlink.Link, error) {
		t.Fatalf("expected linkByIndex not to be called, got %d", index)
		return nil, nil
	})

	err = nil
	_, err = hostDefaultOutboundInterface()
	if err == nil {
		t.Fatal("expected error for missing default route")
	}
}

func useRouteList(t *testing.T, routes []netlink.Route, err error) {
	t.Helper()

	old := routeList
	routeList = func(link netlink.Link, family int) ([]netlink.Route, error) {
		if link != nil {
			t.Fatalf("expected nil link filter, got %v", link)
		}
		if family != netlink.FAMILY_V4 {
			t.Fatalf("expected IPv4 route family, got %d", family)
		}

		return routes, err
	}
	t.Cleanup(func() {
		routeList = old
	})
}

func useLinkByIndex(t *testing.T, lookup func(index int) (netlink.Link, error)) {
	t.Helper()

	old := linkByIndex
	linkByIndex = lookup
	t.Cleanup(func() {
		linkByIndex = old
	})
}

func duplicateFD(file *os.File) (int, error) {
	return syscall.Dup(int(file.Fd()))
}
