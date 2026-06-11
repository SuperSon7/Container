package main

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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

func duplicateFD(file *os.File) (int, error) {
	return syscall.Dup(int(file.Fd()))
}
