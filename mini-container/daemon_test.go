package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mini-container/container/network"
)

func testDaemon(t *testing.T) *daemon {
	t.Helper()
	networks, err := network.NewManager(network.Config{})
	if err != nil {
		t.Fatal(err)
	}
	d := &daemon{containers: make(map[string]*managedContainer), networks: networks, rootfs: "/test-rootfs", statePath: filepath.Join(t.TempDir(), "containers.json")}
	d.launch = func(id, rootfs string, args []string, files []*os.File, _ *network.Manager) (*exec.Cmd, func() error, error) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = files[0], files[1], files[2]
		if err := cmd.Start(); err != nil {
			return nil, nil, err
		}
		return cmd, func() error { return nil }, nil
	}
	t.Cleanup(func() {
		if err := d.shutdown(); err != nil {
			t.Error(err)
		}
	})
	return d
}

func testStdio(t *testing.T) []*os.File {
	t.Helper()
	file, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return []*os.File{file, file, file}
}

func awaitContainer(t *testing.T, c *managedContainer) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("container did not finish")
	}
}

func TestDaemonRunRestartAndPersistDefinition(t *testing.T) {
	d := testDaemon(t)
	files := testStdio(t)
	c, err := d.start(request{Action: "run", Args: []string{"/bin/sh", "-c", "exit 7"}}, files)
	if err != nil {
		t.Fatal(err)
	}
	awaitContainer(t, c)
	if c.exitCode != 7 {
		t.Fatalf("exit code %d", c.exitCode)
	}
	restarted, err := d.start(request{Action: "start", ID: c.ID}, files)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.ID != c.ID || restarted.done == c.done {
		t.Fatal("restart did not preserve identity with a fresh execution")
	}
	awaitContainer(t, restarted)
	loaded := testDaemon(t)
	loaded.statePath = d.statePath
	if err := loaded.load(); err != nil {
		t.Fatal(err)
	}
	info := loaded.list()
	if len(info) != 1 || info[0].ID != c.ID || info[0].Running {
		t.Fatalf("restored definitions: %+v", info)
	}
	if err := d.remove(c.ID); err != nil {
		t.Fatal(err)
	}
	if len(d.list()) != 0 {
		t.Fatal("definition not removed")
	}
}

func TestDaemonStopKeepsDefinitionAndCleansOnce(t *testing.T) {
	d := testDaemon(t)
	launch := d.launch
	var cleanups atomic.Int32
	d.launch = func(id, rootfs string, args []string, files []*os.File, m *network.Manager) (*exec.Cmd, func() error, error) {
		cmd, _, err := launch(id, rootfs, args, files, m)
		return cmd, func() error { cleanups.Add(1); return nil }, err
	}
	c, err := d.start(request{Action: "run", Args: []string{"/bin/sleep", "60"}}, testStdio(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.remove(c.ID); err == nil {
		t.Fatal("removed running container")
	}
	if _, err := d.start(request{Action: "start", ID: c.ID}, testStdio(t)); err == nil {
		t.Fatal("started running container")
	}
	if err := d.stop(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.stop(c.ID); err != nil {
		t.Fatal(err)
	}
	if cleanups.Load() != 1 {
		t.Fatalf("cleanup count %d", cleanups.Load())
	}
	info := d.list()
	if len(info) != 1 || info[0].Running {
		t.Fatalf("stopped definition lost: %+v", info)
	}
	if err := d.remove(c.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonUsesDistinctIDs(t *testing.T) {
	d := testDaemon(t)
	files := testStdio(t)
	var ids []string
	for i := 0; i < 2; i++ {
		c, err := d.start(request{Action: "run", Args: []string{"/bin/true"}}, files)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		awaitContainer(t, c)
	}
	if ids[0] == ids[1] {
		t.Fatal("duplicate container IDs")
	}
}

func TestUnixSocketPassesStdioAndExitStatus(t *testing.T) {
	d := testDaemon(t)
	path := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := listenDaemon(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.AcceptUnix()
		if err != nil {
			t.Error(err)
			return
		}
		d.handle(conn)
	}()
	client, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stdin := testStdio(t)[0]
	data, _ := json.Marshal(request{Action: "run", Args: []string{"/bin/sh", "-c", "printf test-output; exit 9"}})
	_, _, err = client.WriteMsgUnix(data, syscall.UnixRights(int(stdin.Fd()), int(stdout.Fd()), int(stdout.Fd())), nil)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for i := 0; i < 2; i++ {
		data := make([]byte, 4096)
		n, err := client.Read(data)
		if err != nil {
			t.Fatal(err)
		}
		var r response
		if err := json.Unmarshal(data[:n], &r); err != nil {
			t.Fatal(err)
		}
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		if i == 0 {
			id = r.ID
			if id == "" || r.Done {
				t.Fatalf("bad start response %+v", r)
			}
		}
		if i == 1 && (!r.Done || r.ID != id || r.ExitCode != 9) {
			t.Fatalf("bad completion %+v", r)
		}
	}
	<-serverDone
	if _, err := stdout.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil || string(output) != "test-output" {
		t.Fatalf("stdout: %q %v", output, err)
	}
}

func TestSecondDaemonCannotReplaceLiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := listenDaemon(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if second, err := listenDaemon(path); err == nil {
		second.Close()
		t.Fatal("replaced a live socket")
	}
}

func TestFailedLaunchCleansOnceAndKeepsDefinition(t *testing.T) {
	d := testDaemon(t)
	setupErr := errors.New("setup failed")
	cleanups := 0
	d.launch = func(string, string, []string, []*os.File, *network.Manager) (*exec.Cmd, func() error, error) {
		return nil, func() error { cleanups++; return nil }, setupErr
	}
	if _, err := d.start(request{Action: "run", Args: []string{"/bin/true"}}, testStdio(t)); !errors.Is(err, setupErr) {
		t.Fatalf("got %v", err)
	}
	info := d.list()
	if len(info) != 1 || info[0].Running {
		t.Fatalf("failed definition: %+v", info)
	}
	if err := d.remove(info[0].ID); err != nil {
		t.Fatal(err)
	}
	if cleanups != 1 {
		t.Fatalf("cleanup called %d times", cleanups)
	}
}
