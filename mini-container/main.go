package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"mini-container/container/cgroups"
)

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

func run() {
	fmt.Printf("Running %v \n", os.Args[2:])

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS,
	}

	must(cmd.Start())
	manager, err := applyCgroup(cmd.Process.Pid)
	must(err)

	// Wait reports non-zero container exits as errors; translate them after cleanup.
	waitErr := cmd.Wait()
	must(manager.Destroy())
	exitWithChildStatus(waitErr)
}

func child() {
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
