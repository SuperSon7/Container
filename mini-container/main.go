package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"mini-container/container/cgroups"
	"mini-container/container/network"
)

const (
	// ExtraFiles start at fd 3 in the child because 0, 1, and 2 are stdio.
	childSyncFD  = 3
	childSyncEnv = "MINI_CONTAINER_CHILD_SYNC_FD"
)

// main dispatches the parent runtime path and the re-executed child bootstrap path.
func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: mini-container run <command> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()
	default:
		fmt.Println("unknown command:", os.Args[1])
	}
}

// run starts the container bootstrap process, configures host-owned resources,
// then releases the child so it can finish setup and exec the requested command.
func run() {
	fmt.Printf("Running %v \n", os.Args[2:])

	// The child blocks on this pipe before touching its container setup.
	// That gives the parent time to configure the child's new network namespace.
	childSyncRead, childSyncWrite, err := os.Pipe()
	must(err)
	defer childSyncRead.Close()
	defer childSyncWrite.Close()

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// ExtraFiles passes the read side into the child as fd 3.
	cmd.ExtraFiles = []*os.File{childSyncRead}
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", childSyncEnv, childSyncFD))

	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CLONE_NEWNET creates an empty network namespace for the child.
		// The child must stay blocked until the parent attaches eth0 and routes.
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET,
	}

	must(cmd.Start())
	// The parent only writes the release signal; keeping this read end open
	// would hide pipe-close failures from the child.
	must(childSyncRead.Close())

	manager, err := applyCgroup(cmd.Process.Pid)
	if err != nil {
		abortChild(cmd, childSyncWrite)
		must(err)
	}

	if err := setupNetwork(cmd.Process.Pid); err != nil {
		_ = manager.Destroy()
		abortChild(cmd, childSyncWrite)
		must(err)
	}

	must(releaseChild(childSyncWrite))

	// Wait reports non-zero container exits as errors; translate them after cleanup.
	waitErr := cmd.Wait()
	must(manager.Destroy())
	exitWithChildStatus(waitErr)
}

// child waits for parent-side setup, then builds the container view and execs the command.
func child() {
	must(waitForParentSync())
	must(os.Unsetenv(childSyncEnv))

	fmt.Printf("Running %v \n", os.Args[2:])

	must(syscall.Sethostname([]byte("container")))
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))
	// TODO: pass rootfs through runtime config instead of hardcoding a local path.
	must(syscall.Chroot("/home/vanillab/container/rootfs")) // must change as your own path
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
	command, err := exec.LookPath(os.Args[2])
	must(err)

	// TODO: add an optional init wrapper if mini-container should reap or forward signals itself.
	// Exec replaces this bootstrap process, making the requested command PID 1.
	must(syscall.Exec(command, os.Args[2:], os.Environ()))
}

// applyCgroup attaches the container process to the pids-limited cgroup.
func applyCgroup(pid int) (cgroups.Manager, error) {
	// TODO: move cgroup name and resource limits into runtime config.
	pidsLimit := int64(20)
	manager, err := cgroups.NewManager("mini-container/jb")
	if err != nil {
		return nil, err
	}

	if err := manager.Set(cgroups.ResourceConfig{PidsLimit: &pidsLimit}); err != nil {
		return nil, err
	}
	if err := manager.Apply(pid); err != nil {
		return nil, err
	}

	return manager, nil
}

// setupNetwork connects the child process's network namespace to the host bridge.
func setupNetwork(pid int) error {
	// TODO: move network settings into runtime config.
	manager := network.NewManager(network.Config{
		Enabled:           true,
		BridgeName:        "mini0",
		ContainerAddress:  "10.0.0.2/24",
		GatewayAddress:    "10.0.0.1/24",
		EnableNAT:         true,
		OutboundInterface: "eth0",
	})

	return manager.Setup(pid)
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
	if _, err := syncWrite.Write([]byte{1}); err != nil {
		return fmt.Errorf("release child: %w", err)
	}

	if err := syncWrite.Close(); err != nil {
		return fmt.Errorf("close child sync pipe: %w", err)
	}

	return nil
}

// abortChild closes the sync pipe, kills the blocked child, and reaps it.
func abortChild(cmd *exec.Cmd, syncWrite *os.File) {
	_ = syncWrite.Close()
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

// exitWithChildStatus mirrors the container process status instead of panicking on normal exits.
func exitWithChildStatus(err error) {
	if err == nil {
		return
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		must(err)
	}

	if code := exitErr.ExitCode(); code >= 0 {
		os.Exit(code)
	}

	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		os.Exit(128 + int(status.Signal()))
	}

	must(err)
}
