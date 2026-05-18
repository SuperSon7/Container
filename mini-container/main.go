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

	must(cmd.Run())
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
	must(syscall.Mount("tmpfs", "/mytemp", "tmpfs", 0, ""))

	cg()

	must(cmd.Run())

	defer must(syscall.Unmount("proc", 0))
	defer must(syscall.Unmount("mytemp", 0))
}

func cg() {
	cgroups := "/sys/fs/cgroup"
	pids := filepath.Join(cgroups, "pids")
	os.MkdirAll(filepath.Join(pids, "jb"), 0755)
	must(os.WriteFile(filepath.Join(pids, "jb/pids.max"), []byte("20"), 0700))
	// Removes the new cgroup in place after the container exits
	must(os.WriteFile(filepath.Join(pids, "jb/notify_on_release"), []byte("1"), 0700))
	must(os.WriteFile(filepath.Join(pids, "jb/cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0700))

}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
