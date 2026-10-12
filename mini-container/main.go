package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"mini-container/container/cgroups"
	"mini-container/container/network"

	"github.com/vishvananda/netlink"
)

const (
	// ExtraFiles start at fd 3 in the child because 0, 1, and 2 are stdio.
	childSyncFD  = 3
	childSyncEnv = "MINI_CONTAINER_CHILD_SYNC_FD"
)

var (
	// routeList is replaceable in tests so default-route detection does not depend on the host.
	routeList = netlink.RouteList

	// linkByIndex is replaceable in tests alongside routeList.
	linkByIndex = netlink.LinkByIndex
)

// main dispatches the daemon, CLI client and re-executed child bootstrap.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mini-container daemon --rootfs <path> | run <command> [args...] | start/stop/rm <id> | ps | network-rm")
		os.Exit(1)
	}
	if os.Args[1] == "child" {
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "child requires rootfs and command")
			os.Exit(1)
		}
		child()
		return
	}
	var code int
	var err error
	if os.Args[1] == "daemon" {
		err = serveDaemon(os.Args[2:])
	} else {
		code, err = runClient(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

// startContainer leaves the bootstrap blocked until cgroup and networking are ready.
// The daemon owns the process and calls cleanup only after it has been reaped.
func startContainer(id, rootfs string, args []string, stdio []*os.File, networks *network.Manager) (*exec.Cmd, func() error, error) {
	childSyncRead, childSyncWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer childSyncRead.Close()
	defer childSyncWrite.Close()
	cmd := exec.Command("/proc/self/exe", append([]string{"child", rootfs}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio[0], stdio[1], stdio[2]
	cmd.ExtraFiles = []*os.File{childSyncRead}
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", childSyncEnv, childSyncFD))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
		Pdeathsig:  syscall.SIGKILL,
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	var cgroupManager cgroups.Manager
	cleanup := func() error {
		networkErr := networks.Detach(id)
		var cgroupErr error
		if cgroupManager != nil {
			cgroupErr = cgroupManager.Destroy()
		}
		return errors.Join(networkErr, cgroupErr)
	}
	cgroupManager, err = applyCgroup(id, cmd.Process.Pid)
	if err == nil {
		_, err = networks.Attach(id, cmd.Process.Pid)
	}
	if err == nil {
		err = releaseChild(childSyncWrite)
	}
	if err != nil {
		abortChild(cmd, childSyncWrite)
		return nil, cleanup, err
	}
	return cmd, cleanup, nil
}

// child waits for parent-side setup, then builds the container view and execs the command.
func child() {
	must(waitForParentSync())
	must(os.Unsetenv(childSyncEnv))

	fmt.Printf("Running %v \n", os.Args[3:])

	must(syscall.Sethostname([]byte("container")))
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))
	must(syscall.Chroot(os.Args[2]))
	must(os.Chdir("/"))

	must(syscall.Mount("proc", "/proc", "proc", 0, ""))
	defer func() {
		must(syscall.Unmount("/proc", 0))
	}()

	must(syscall.Mount("tmpfs", "/mytemp", "tmpfs", 0, ""))
	defer func() {
		must(syscall.Unmount("/mytemp", 0))
	}()

	// Resolve PATH after chroot so lookup happens against the container rootfs.
	command, err := exec.LookPath(os.Args[3])
	must(err)

	// TODO: add an optional init wrapper if mini-container should reap or forward signals itself.
	// Exec replaces this bootstrap process, making the requested command PID 1.
	must(syscall.Exec(command, os.Args[3:], os.Environ()))
}

// applyCgroup attaches the container process to the pids-limited cgroup.
func applyCgroup(id string, pid int) (cgroups.Manager, error) {
	// TODO: move resource limits into runtime config.
	pidsLimit := int64(20)
	cgroupManager, err := cgroups.NewManager("mini-container/" + id)
	if err != nil {
		return nil, err
	}

	if err := cgroupManager.Set(cgroups.ResourceConfig{PidsLimit: &pidsLimit}); err != nil {
		return cgroupManager, err
	}
	if err := cgroupManager.Apply(pid); err != nil {
		return cgroupManager, err
	}

	return cgroupManager, nil
}

// newNetworkManager returns the static bridge network used by this prototype runtime.
func newNetworkManager() (*network.Manager, error) {
	outboundInterface, err := hostDefaultOutboundInterface()
	if err != nil {
		return nil, err
	}

	// TODO: move network settings into runtime config.
	return network.NewManager(network.Config{
		Enabled:           true,
		BridgeName:        "mini0",
		GatewayAddress:    "10.0.0.1/24",
		EnableNAT:         true,
		OutboundInterface: outboundInterface,
	})
}

// hostDefaultOutboundInterface finds the host interface used by the default IPv4 route.
func hostDefaultOutboundInterface() (string, error) {
	routes, err := routeList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "", fmt.Errorf("list host IPv4 routes: %w", err)
	}

	for _, route := range routes {
		if !isDefaultIPv4Route(route) {
			continue
		}

		link, err := linkByIndex(route.LinkIndex)
		if err != nil {
			return "", fmt.Errorf("find default route link %d: %w", route.LinkIndex, err)
		}
		if link.Attrs().Name == "" {
			return "", fmt.Errorf("network: default route link %d has empty name", route.LinkIndex)
		}

		return link.Attrs().Name, nil
	}

	return "", fmt.Errorf("network: default outbound interface not found")
}

// isDefaultIPv4Route accepts both netlink encodings seen for an IPv4 default route.
func isDefaultIPv4Route(route netlink.Route) bool {
	if route.Dst == nil {
		return true
	}

	ones, bits := route.Dst.Mask.Size()
	return bits == 32 && ones == 0 && route.Dst.IP.Equal(net.IPv4zero)
}

// waitForParentSync blocks the child until the parent finishes cgroup/network setup.
func waitForParentSync() error {
	fdValue := os.Getenv(childSyncEnv)
	if fdValue == "" {
		return nil
	}

	fd, err := strconv.Atoi(fdValue)
	if err != nil {
		return fmt.Errorf("parse child sync fd %q: %w", fdValue, err)
	}

	file := os.NewFile(uintptr(fd), "child-sync")
	if file == nil {
		return fmt.Errorf("open child sync fd %d", fd)
	}
	defer file.Close()

	var buf [1]byte
	// ReadFull keeps the child stopped until the parent writes the release byte.
	if _, err := io.ReadFull(file, buf[:]); err != nil {
		return fmt.Errorf("wait for parent setup: %w", err)
	}

	return nil
}

// releaseChild sends the one-byte signal that lets the child continue.
func releaseChild(syncWrite *os.File) error {
	_, writeErr := syncWrite.Write([]byte{1})
	closeErr := syncWrite.Close()

	if writeErr != nil {
		return fmt.Errorf("release child: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close child sync pipe: %w", closeErr)
	}

	return nil
}

// abortChild closes the optional sync pipe, kills the blocked child, and reaps it.
func abortChild(cmd *exec.Cmd, syncWrite *os.File) {
	if syncWrite != nil {
		_ = syncWrite.Close()
	}
	if cmd.Process == nil {
		return
	}

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// must keeps the prototype runtime path compact by panicking on unexpected setup errors.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// childExitCode preserves the application's exit status across the CLI connection.
func childExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
	}
	return 1
}
