package app_test

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reelnest/internal/app"
)

// A subprocess runs the real HTTP boundary, enabling an abrupt crash without
// injecting internal scanner hooks or manipulating the database from a test.
func TestMain(m *testing.M) {
	if os.Getenv("REELNEST_SCAN_TEST_CHILD") == "1" {
		application, err := app.New(app.Config{DataDir: os.Getenv("REELNEST_DATA"), Username: "family", Password: password, MediaRoots: []string{os.Getenv("REELNEST_MEDIA")}})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("http://" + listener.Addr().String())
		_ = http.Serve(listener, application.Handler())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func startProcess(t *testing.T, data, root string) (*runningApp, func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "REELNEST_SCAN_TEST_CHILD=1", "REELNEST_DATA="+data, "REELNEST_MEDIA="+root)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			stopped = true
		}
	}
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	var address string
	select {
	case address = <-ready:
		if len(address) < 2 {
			t.Fatal("child failed to start")
		}
		address = address[:len(address)-1]
	case <-time.After(15 * time.Second):
		t.Fatal("child startup timed out")
	}
	jar, _ := cookiejar.New(nil)
	return &runningApp{server: &httptest.Server{URL: address}, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}}, stop
}

func TestInterruptedScanRetainsCommittedLibraryAfterProcessRestart(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	writeMedia(t, filepath.Join(root, "original.mp4"), "committed fixture")
	a, kill := startProcess(t, data, root)
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	original := library(t, a)[0]
	for index := 0; index < 6000; index++ {
		writeMedia(t, filepath.Join(root, "new", fmt.Sprintf("%05d.mp4", index)), "isolated crash fixture")
	}
	rescan(t, a, d.ID)
	// The 202 response establishes an accepted running scan; kill the real process
	// before its many-file traversal can publish a new complete snapshot.
	kill()
	b, _ := startProcess(t, data, root)
	login(t, b)
	awaitScan(t, b, d.ID, "interrupted")
	items := library(t, b)
	if len(items) != 1 || items[0].ID != original.ID || !items[0].Available {
		t.Fatalf("incomplete scan changed committed index: %+v", items)
	}
	rescan(t, b, d.ID)
	awaitScan(t, b, d.ID, "completed")
	if items = library(t, b); len(items) != 6001 {
		t.Fatalf("manual recovery collected %d files", len(items))
	}
}
