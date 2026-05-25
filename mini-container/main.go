package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
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
			syscall.CLONE_NEWNS, //mount_namespace
	}

	must(cmd.Start())
	must(applyCgroup(cmd.Process.Pid))
	must(cmd.Wait())
}

func child() {
	fmt.Printf("Running %v \n", os.Args[2:])

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	must(syscall.Sethostname([]byte("container")))
	must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))
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

	must(cmd.Run())
}

func applyCgroup(pid int) error {

	cgroupPath := "/sys/fs/cgroup/pids/jb"

	must(os.MkdirAll(cgroupPath, 0755))
	must(os.WriteFile(filepath.Join(cgroupPath, "pids.max"), []byte("20"), 0700))
	// Removes the new cgroup in place after the container exits
	must(os.WriteFile(filepath.Join(cgroupPath, "notify_on_release"), []byte("1"), 0700))
	must(os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0700))

	return nil

}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
