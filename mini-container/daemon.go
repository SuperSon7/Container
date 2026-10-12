package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"mini-container/container/network"
)

const defaultSocket = "/run/mini-container.sock"

// The CLI passes its stdio descriptors over a local Unix socket. The daemon
// remains the parent of the container; output need not be proxied through it.
type request struct {
	Action string
	ID     string
	Args   []string
}

type response struct {
	ID         string          `json:",omitempty"`
	Containers []containerInfo `json:",omitempty"`
	ExitCode   int
	Done       bool
	Error      string `json:",omitempty"`
}

type definition struct {
	ID     string
	Args   []string
	Rootfs string
}

type containerInfo struct {
	ID       string
	Args     []string
	Running  bool
	ExitCode int
}

type managedContainer struct {
	definition
	cmd      *exec.Cmd
	done     chan struct{}
	cleanup  func() error
	exitCode int
	err      error
}

type daemon struct {
	mu         sync.Mutex
	containers map[string]*managedContainer
	networks   *network.Manager
	rootfs     string
	statePath  string
	closing    bool
	launch     func(string, string, []string, []*os.File, *network.Manager) (*exec.Cmd, func() error, error)
}

func socketPath() string {
	if path := os.Getenv("MINI_CONTAINER_SOCKET"); path != "" {
		return path
	}
	return defaultSocket
}

func newContainerID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func serveDaemon(args []string) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	rootfs := flags.String("rootfs", "", "container root filesystem")
	stateDir := flags.String("state-dir", "/var/lib/mini-container", "container definitions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rootfs == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: mini-container daemon --rootfs <path> [--state-dir <path>]")
	}
	absoluteRootfs, err := filepath.Abs(*rootfs)
	if err != nil {
		return err
	}
	for _, path := range []string{absoluteRootfs, filepath.Join(absoluteRootfs, "proc"), filepath.Join(absoluteRootfs, "mytemp")} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("rootfs directory required: %s", path)
		}
	}
	if err := os.MkdirAll(*stateDir, 0700); err != nil {
		return err
	}
	// A second daemon must not load the same definitions into another allocator.
	lock, err := os.OpenFile(filepath.Join(*stateDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("daemon already owns state directory: %w", err)
	}
	listener, err := listenDaemon(socketPath())
	if err != nil {
		return err
	}
	defer listener.Close()
	networks, err := newNetworkManager()
	if err != nil {
		return err
	}
	d := &daemon{containers: make(map[string]*managedContainer), networks: networks, rootfs: absoluteRootfs,
		statePath: filepath.Join(*stateDir, "containers.json"), launch: startContainer}
	if err := d.load(); err != nil {
		return err
	}
	if err := networks.Setup(); err != nil {
		return err
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-stop:
			listener.Close()
		case <-finished:
		}
	}()
	fmt.Fprintf(os.Stderr, "daemon listening on %s\n", socketPath())
	var handlers sync.WaitGroup
	var acceptErr error
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				acceptErr = err
			}
			break
		}
		handlers.Add(1)
		go func() { defer handlers.Done(); d.handle(conn) }()
	}
	shutdownErr := d.shutdown()
	handlers.Wait()
	return errors.Join(acceptErr, shutdownErr)
}

func listenDaemon(path string) (*net.UnixListener, error) {
	// Never unlink another live daemon's socket. A stale socket from a crash can
	// be removed; ListenUnix still arbitrates concurrent startup attempts.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket path is not a socket: %s", path)
		}
		conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: path, Net: "unixpacket"})
		if err == nil {
			conn.Close()
			return nil, fmt.Errorf("daemon already listening on %s", path)
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, err
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func (d *daemon) handle(conn *net.UnixConn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	req, files, err := receiveRequest(conn)
	for _, file := range files {
		defer file.Close()
	}
	send := func(r response) { _ = json.NewEncoder(conn).Encode(r) }
	if err != nil {
		send(response{Done: true, Error: err.Error()})
		return
	}
	conn.SetReadDeadline(time.Time{})
	switch req.Action {
	case "run", "start":
		c, err := d.start(req, files)
		if err != nil {
			send(response{Done: true, Error: err.Error()})
			return
		}
		send(response{ID: c.ID})
		<-c.done
		d.mu.Lock()
		r := response{ID: c.ID, Done: true, ExitCode: c.exitCode}
		if c.err != nil {
			r.Error = c.err.Error()
		}
		d.mu.Unlock()
		send(r)
	case "stop":
		err = d.stop(req.ID)
	case "rm":
		err = d.remove(req.ID)
	case "ps":
		send(response{Done: true, Containers: d.list()})
		return
	case "network-rm":
		err = d.networks.Destroy()
	default:
		err = fmt.Errorf("unknown action: %s", req.Action)
	}
	r := response{Done: true}
	if err != nil {
		r.Error = err.Error()
	}
	send(r)
}

func (d *daemon) start(req request, files []*os.File) (*managedContainer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closing {
		return nil, fmt.Errorf("daemon is stopping")
	}
	if len(files) != 3 {
		return nil, fmt.Errorf("run/start requires three stdio descriptors")
	}
	var c *managedContainer
	if req.Action == "run" {
		if len(req.Args) == 0 {
			return nil, fmt.Errorf("run requires a command")
		}
		id, err := newContainerID()
		if err != nil {
			return nil, err
		}
		c = &managedContainer{definition: definition{ID: id, Args: req.Args, Rootfs: d.rootfs}}
		d.containers[id] = c
		if err := d.save(); err != nil {
			delete(d.containers, id)
			return nil, err
		}
	} else {
		c = d.containers[req.ID]
		if c == nil {
			return nil, fmt.Errorf("unknown container: %s", req.ID)
		}
		if c.cmd != nil {
			return nil, fmt.Errorf("container %s is running", c.ID)
		}
		if c.cleanup != nil {
			if err := c.cleanup(); err != nil {
				return nil, err
			}
		}
		// Keep each execution's completion channel and result separate so an
		// old attached CLI cannot accidentally wait on a later restart.
		c = &managedContainer{definition: c.definition}
		d.containers[c.ID] = c
	}
	cmd, cleanup, err := d.launch(c.ID, c.Rootfs, c.Args, files, d.networks)
	c.cleanup = cleanup
	if err != nil {
		if cleanup != nil {
			cleanupErr := cleanup()
			if cleanupErr == nil {
				c.cleanup = nil
			}
			err = errors.Join(err, cleanupErr)
		}
		return nil, fmt.Errorf("start container %s: %w", c.ID, err)
	}
	c.cmd, c.done, c.err = cmd, make(chan struct{}), nil
	go func() {
		waitErr := cmd.Wait()
		cleanupErr := cleanup()
		d.mu.Lock()
		defer d.mu.Unlock()
		c.exitCode, c.err, c.cmd = childExitCode(waitErr), cleanupErr, nil
		if cleanupErr == nil {
			c.cleanup = nil
		}
		close(c.done)
	}()
	return c, nil
}

func (d *daemon) stop(id string) error {
	d.mu.Lock()
	c := d.containers[id]
	if c == nil {
		d.mu.Unlock()
		return fmt.Errorf("unknown container: %s", id)
	}
	if c.cmd == nil {
		d.mu.Unlock()
		return nil
	}
	process, done := c.cmd.Process, c.done
	signalErr := process.Signal(syscall.SIGTERM)
	d.mu.Unlock()
	if signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
		return signalErr
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-done
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return c.err
}

func (d *daemon) remove(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.containers[id]
	if c == nil {
		return fmt.Errorf("unknown container: %s", id)
	}
	if c.cmd != nil {
		return fmt.Errorf("stop container %s before removing it", id)
	}
	if c.cleanup != nil {
		if err := c.cleanup(); err != nil {
			return err
		}
	}
	delete(d.containers, id)
	if err := d.save(); err != nil {
		d.containers[id] = c
		return err
	}
	return nil
}

func (d *daemon) list() []containerInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]containerInfo, 0, len(d.containers))
	for _, c := range d.containers {
		result = append(result, containerInfo{ID: c.ID, Args: c.Args, Running: c.cmd != nil, ExitCode: c.exitCode})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// Containers do not outlive this daemon: graceful shutdown reaps them, and
// Pdeathsig kills their PID 1 on an abrupt daemon exit. Definitions survive.
func (d *daemon) shutdown() error {
	d.mu.Lock()
	d.closing = true
	var ids []string
	for id := range d.containers {
		ids = append(ids, id)
	}
	d.mu.Unlock()
	var errs []error
	for _, id := range ids {
		errs = append(errs, d.stop(id))
	}
	return errors.Join(errs...)
}

func (d *daemon) save() error {
	defs := make([]definition, 0, len(d.containers))
	for _, c := range d.containers {
		defs = append(defs, c.definition)
	}
	data, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, d.statePath)
}

func (d *daemon) load() error {
	data, err := os.ReadFile(d.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var defs []definition
	if err := json.Unmarshal(data, &defs); err != nil {
		return err
	}
	for _, def := range defs {
		// IDs become cgroup paths, so reject malformed on-disk definitions.
		id, err := hex.DecodeString(def.ID)
		if err != nil || len(id) != 16 || len(def.Args) == 0 || !filepath.IsAbs(def.Rootfs) {
			return fmt.Errorf("invalid saved container definition: %q", def.ID)
		}
		d.containers[def.ID] = &managedContainer{definition: def}
	}
	return nil
}

func receiveRequest(conn *net.UnixConn) (request, []*os.File, error) {
	data := make([]byte, 64*1024)
	control := make([]byte, syscall.CmsgSpace(3*4))
	n, oobn, flags, _, err := conn.ReadMsgUnix(data, control)
	if err != nil {
		return request{}, nil, err
	}
	var files []*os.File
	messages, err := syscall.ParseSocketControlMessage(control[:oobn])
	if err != nil {
		return request{}, nil, err
	}
	for _, message := range messages {
		fds, err := syscall.ParseUnixRights(&message)
		if err != nil {
			return request{}, files, err
		}
		for _, fd := range fds {
			syscall.CloseOnExec(fd)
			files = append(files, os.NewFile(uintptr(fd), "client-stdio"))
		}
	}
	if flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0 {
		return request{}, files, fmt.Errorf("request too large")
	}
	var req request
	err = json.Unmarshal(data[:n], &req)
	return req, files, err
}

func runClient(args []string) (int, error) {
	req := request{Action: args[0]}
	switch req.Action {
	case "run":
		if len(args) < 2 {
			return 1, fmt.Errorf("run requires a command")
		}
		req.Args = args[1:]
	case "start", "stop", "rm":
		if len(args) != 2 {
			return 1, fmt.Errorf("%s requires a container ID", req.Action)
		}
		req.ID = args[1]
	case "ps", "network-rm":
		if len(args) != 1 {
			return 1, fmt.Errorf("unexpected arguments")
		}
	default:
		return 1, fmt.Errorf("unknown command: %s", req.Action)
	}
	conn, err := net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: socketPath(), Net: "unixpacket"})
	if err != nil {
		return 1, fmt.Errorf("connect to daemon (start it with 'daemon --rootfs <path>'): %w", err)
	}
	defer conn.Close()
	data, err := json.Marshal(req)
	if err != nil {
		return 1, err
	}
	if len(data) > 64*1024 {
		return 1, fmt.Errorf("request too large")
	}
	var rights []byte
	if req.Action == "run" || req.Action == "start" {
		rights = syscall.UnixRights(0, 1, 2)
	}
	if _, _, err := conn.WriteMsgUnix(data, rights, nil); err != nil {
		return 1, err
	}
	// Each response is one packet; use a full-size buffer rather than a stream
	// decoder, which could discard the tail of a Unix sequenced packet.
	for {
		packet := make([]byte, 1024*1024)
		n, _, flags, _, err := conn.ReadMsgUnix(packet, nil)
		if err != nil {
			return 1, err
		}
		if n == 0 {
			return 1, io.EOF
		}
		if flags&syscall.MSG_TRUNC != 0 {
			return 1, fmt.Errorf("daemon response too large")
		}
		var r response
		if err := json.Unmarshal(packet[:n], &r); err != nil {
			return 1, err
		}
		if r.Error != "" {
			return 1, errors.New(r.Error)
		}
		if !r.Done {
			fmt.Fprintln(os.Stderr, "container:", r.ID)
			// The CLI is in the user's foreground process group, while the
			// container belongs to the daemon. Forward interruption as stop.
			interrupt := make(chan os.Signal, 1)
			signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(interrupt)
			finished := make(chan struct{})
			defer close(finished)
			go func(id string) {
				select {
				case <-interrupt:
					_, _ = runClient([]string{"stop", id})
				case <-finished:
				}
			}(r.ID)
			continue
		}
		for _, c := range r.Containers {
			state := "stopped"
			if c.Running {
				state = "running"
			}
			fmt.Printf("%s\t%s\t%v\n", c.ID, state, c.Args)
		}
		return r.ExitCode, nil
	}
}
