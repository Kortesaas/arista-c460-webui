package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func pcapFixture(order binary.ByteOrder, nano bool, payload int, packets int) []byte {
	buf := make([]byte, 24)
	magic := uint32(0xa1b2c3d4)
	if nano {
		magic = 0xa1b23c4d
	}
	order.PutUint32(buf, magic)
	order.PutUint16(buf[4:], 2)
	order.PutUint16(buf[6:], 4)
	order.PutUint32(buf[16:], 128)
	order.PutUint32(buf[20:], 1)
	for n := 0; n < packets; n++ {
		record := make([]byte, 16+payload)
		order.PutUint32(record, 1)
		order.PutUint32(record[8:], uint32(payload))
		order.PutUint32(record[12:], uint32(payload))
		for i := 16; i < len(record); i++ {
			record[i] = byte(n)
		}
		buf = append(buf, record...)
	}
	return buf
}

func TestPCAPFramingAndExactLimit(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, nano := range []bool{false, true} {
			for _, chunk := range []int{1, 7, 24, 65536} {
				raw := pcapFixture(order, nano, 64, 4)
				var out bytes.Buffer
				var bytesWritten, packets int64
				sink := &pcapSink{out: &out, maxSnap: 128, limit: 190, progress: func(b, p int64, _ bool) { bytesWritten, packets = b, p }}
				var err error
				for start := 0; start < len(raw) && err == nil; start += chunk {
					_, err = sink.Write(raw[start:min(start+chunk, len(raw))])
				}
				if !errors.Is(err, errCaptureSize) || bytesWritten != 184 || packets != 2 || !bytes.Equal(out.Bytes(), raw[:184]) {
					t.Fatalf("bad record-boundary cap: chunk=%d bytes=%d packets=%d err=%v", chunk, bytesWritten, packets, err)
				}
			}
		}
	}
}

func TestPCAPRejectsMalformedAndShortWrites(t *testing.T) {
	valid := pcapFixture(binary.LittleEndian, false, 64, 1)
	for _, offset := range []int{0, 4, 16, 32} {
		raw := bytes.Clone(valid)
		for i := offset; i < offset+4; i++ {
			raw[i] = 255
		}
		sink := &pcapSink{out: io.Discard, maxSnap: 128, limit: 65536, progress: func(int64, int64, bool) {}}
		if _, err := sink.Write(raw); err == nil {
			t.Fatal("invalid PCAP accepted", offset)
		}
	}
	sink := &pcapSink{out: shortCaptureWriter{}, maxSnap: 128, limit: 65536, progress: func(int64, int64, bool) {}}
	if _, err := sink.Write(valid); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write ignored", err)
	}
	// An interrupted last record never leaks into the downloadable file.
	var out bytes.Buffer
	sink = &pcapSink{out: &out, maxSnap: 128, limit: 65536, progress: func(int64, int64, bool) {}}
	if _, err := sink.Write(valid[:len(valid)-1]); err != nil || out.Len() != 24 {
		t.Fatal("partial record written", err, out.Len())
	}
}

type shortCaptureWriter struct{}

func (shortCaptureWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestCaptureValidationAndFixedArguments(t *testing.T) {
	base := CaptureInput{Interface: "lo"}
	for _, change := range []func(*CaptureInput){
		func(v *CaptureInput) { v.Seconds = -1 }, func(v *CaptureInput) { v.Seconds = 121 },
		func(v *CaptureInput) { v.MaxBytes = 65535 }, func(v *CaptureInput) { v.MaxBytes = captureMaxBytes + 1 },
		func(v *CaptureInput) { v.SnapLength = 63 }, func(v *CaptureInput) { v.SnapLength = 4097 },
		func(v *CaptureInput) { v.Protocol = "tcp; reboot" }, func(v *CaptureInput) { v.Host = "example.org" },
		func(v *CaptureInput) { v.Host = "127.0.0.1 -z reboot" }, func(v *CaptureInput) { v.Port = 65536 },
		func(v *CaptureInput) { v.Protocol, v.Port = "arp", 53 },
	} {
		v := base
		change(&v)
		if _, err := normalizeCapture(v); err == nil {
			t.Fatal("invalid capture accepted", v)
		}
	}
	v, err := normalizeCapture(CaptureInput{Interface: "lo", Protocol: "icmp", Host: "::1"})
	args := captureArgs(v)
	if err != nil || v.MaxBytes != 1<<20 || v.Seconds != 10 || args[len(args)-1] != "(icmp or icmp6) and host ::1" || strings.Contains(strings.Join(args, " "), "-I") {
		t.Fatal("wrong defaults/argv", v, args, err)
	}
}

func fixtureCaptures(t *testing.T, run captureRunner) *PacketCaptures {
	t.Helper()
	c := NewPacketCaptures(context.Background(), filepath.Join(t.TempDir(), "captures"))
	c.available = func() bool { return true }
	c.interfaces = func() []CaptureInterface {
		return []CaptureInterface{{Name: "lo", Up: true}, {Name: "eth0", Up: false}}
	}
	c.run = run
	t.Cleanup(c.Close)
	return c
}

func awaitCapture(t *testing.T, c *PacketCaptures, id string) CaptureJob {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := c.get(id)
		if !ok {
			t.Fatal("job disappeared")
		}
		if job.State != "running" {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("capture failed to stop within deadline")
	return CaptureJob{}
}

func TestCaptureLifecycleCancellationAndExpiry(t *testing.T) {
	c := fixtureCaptures(t, func(ctx context.Context, _ []string, out io.Writer) error {
		_, err := out.Write(pcapFixture(binary.LittleEndian, false, 64, 2))
		if err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	for _, iface := range []string{"eth0", "../../etc/shadow", "lo -I", "not-an-interface"} {
		if _, err := c.start(CaptureInput{Interface: iface}); err == nil {
			t.Fatal("bad interface accepted", iface)
		}
	}
	job, err := c.start(CaptureInput{Interface: "lo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.start(CaptureInput{Interface: "lo"}); !errors.Is(err, errCaptureBusy) {
		t.Fatal("overlap allowed", err)
	}
	if _, err := c.open(job.ID); err == nil {
		t.Fatal("running capture downloaded")
	}
	if _, err := c.remove(job.ID); err == nil {
		t.Fatal("running capture deleted")
	}
	if _, ok := c.stop(job.ID); !ok {
		t.Fatal("stop lost job")
	}
	done := awaitCapture(t, c, job.ID)
	if done.State != "stopped" || done.Reason != "cancelled" || !done.Download || done.Packets != 2 || done.Bytes != 184 {
		t.Fatal(done)
	}
	file, err := c.open(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := file.Stat()
	raw, _ := io.ReadAll(file)
	file.Close()
	if info.Mode().Perm() != 0600 || !bytes.Equal(raw, pcapFixture(binary.LittleEndian, false, 64, 2)) {
		t.Fatal("private/valid file invariant failed")
	}
	c.mu.Lock()
	past := time.Now().Add(-time.Second)
	c.jobs[job.ID].ExpiresAt = &past
	c.mu.Unlock()
	if len(c.list()) != 0 || fileExists(filepath.Join(c.dir, job.ID+".pcap")) {
		t.Fatal("expiry did not clean file and metadata")
	}
}

func TestCaptureSizeTimeAndRetentionBounds(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		c := fixtureCaptures(t, func(_ context.Context, _ []string, out io.Writer) error {
			_, err := out.Write(pcapFixture(binary.LittleEndian, false, 128, 10000))
			return err
		})
		job, err := c.start(CaptureInput{Interface: "lo", MaxBytes: 65536})
		if err != nil {
			t.Fatal(err)
		}
		done := awaitCapture(t, c, job.ID)
		if done.State != "completed" || done.Reason != "size-limit" || done.Bytes > 65536 || done.Bytes < 65000 || !done.Download {
			t.Fatal(done)
		}
	})
	t.Run("time", func(t *testing.T) {
		c := fixtureCaptures(t, func(ctx context.Context, _ []string, out io.Writer) error {
			_, _ = out.Write(pcapFixture(binary.LittleEndian, false, 0, 0))
			<-ctx.Done()
			return ctx.Err()
		})
		job, err := c.start(CaptureInput{Interface: "lo", Seconds: 1})
		if err != nil {
			t.Fatal(err)
		}
		done := awaitCapture(t, c, job.ID)
		if done.State != "completed" || done.Reason != "time-limit" || done.Bytes != 24 || !done.Download {
			t.Fatal(done)
		}
	})
	t.Run("retention", func(t *testing.T) {
		c := fixtureCaptures(t, func(_ context.Context, _ []string, out io.Writer) error {
			_, err := out.Write(pcapFixture(binary.LittleEndian, false, 64, 1))
			return err
		})
		first := ""
		for i := 0; i < 5; i++ {
			job, err := c.start(CaptureInput{Interface: "lo"})
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = job.ID
			}
			awaitCapture(t, c, job.ID)
		}
		files, _ := os.ReadDir(c.dir)
		if len(c.list()) != captureMaxJobs || len(files) != captureMaxJobs || fileExists(filepath.Join(c.dir, first+".pcap")) {
			t.Fatal("retention cap failed")
		}
		c.Close()
		files, _ = os.ReadDir(c.dir)
		if len(files) != 0 {
			t.Fatal("shutdown retained private captures")
		}
		if _, err := c.start(CaptureInput{Interface: "lo"}); !errors.Is(err, errCaptureUnavailable) {
			t.Fatal("shutdown accepted a new job")
		}
	})
}

func TestCaptureConcurrentAdmissionAndFailedTool(t *testing.T) {
	c := fixtureCaptures(t, func(ctx context.Context, _ []string, out io.Writer) error {
		_, _ = out.Write(pcapFixture(binary.LittleEndian, false, 0, 0))
		<-ctx.Done()
		return ctx.Err()
	})
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := []string{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := c.start(CaptureInput{Interface: "lo"})
			if err == nil {
				mu.Lock()
				accepted = append(accepted, job.ID)
				mu.Unlock()
			} else if !errors.Is(err, errCaptureBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(accepted) != 1 {
		t.Fatal("concurrent overlap", accepted)
	}
	c.stop(accepted[0])
	awaitCapture(t, c, accepted[0])
	c.run = func(context.Context, []string, io.Writer) error { return errors.New("vendor password=must-not-escape") }
	job, err := c.start(CaptureInput{Interface: "lo"})
	if err != nil {
		t.Fatal(err)
	}
	done := awaitCapture(t, c, job.ID)
	raw, _ := json.Marshal(done)
	if done.State != "failed" || done.Download || strings.Contains(string(raw), "must-not-escape") || fileExists(filepath.Join(c.dir, job.ID+".pcap")) {
		t.Fatal("failed capture leaked output/file", string(raw))
	}
}

func TestCaptureStartupCleanupAndSymlinkRefusal(t *testing.T) {
	dir := t.TempDir()
	old := strings.Repeat("a", 32) + ".pcap"
	fixtureFile(t, filepath.Join(dir, old), "private")
	fixtureFile(t, filepath.Join(dir, "unrelated"), "keep")
	c := NewPacketCaptures(context.Background(), dir)
	defer c.Close()
	if c.initErr != nil || fileExists(filepath.Join(dir, old)) || !fileExists(filepath.Join(dir, "unrelated")) {
		t.Fatal("startup cleanup scope failed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	unsafe := NewPacketCaptures(context.Background(), link)
	defer unsafe.Close()
	if unsafe.initErr == nil {
		t.Fatal("symlink capture root accepted")
	}
}

func TestCaptureHTTPPermissionsAndDownload(t *testing.T) {
	c := fixtureCaptures(t, func(_ context.Context, _ []string, out io.Writer) error {
		_, err := out.Write(pcapFixture(binary.LittleEndian, false, 64, 1))
		return err
	})
	a := &API{captures: c, auth: NewAuth(filepath.Join(t.TempDir(), "auth.json")), changes: NewChangeLog(""), poller: &Poller{}}
	mux := http.NewServeMux()
	a.Register(mux)
	viewer := httptest.NewRecorder()
	_ = a.auth.NewSession(viewer, "observer", RoleViewer)
	admin := httptest.NewRecorder()
	_ = a.auth.NewSession(admin, "config", RoleAdmin)
	for _, tc := range []struct {
		method, path string
		cookie       *http.Cookie
		want         int
	}{
		{"GET", "/api/captures", nil, 401}, {"GET", "/api/captures", viewer.Result().Cookies()[0], 200},
		{"POST", "/api/captures", viewer.Result().Cookies()[0], 403}, {"GET", "/api/captures/unknown/download", viewer.Result().Cookies()[0], 403},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"interface":"lo"}`))
		r.Header.Set("Content-Type", "application/json")
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/captures", strings.NewReader(`{"interface":"lo","unknown":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(admin.Result().Cookies()[0])
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unknown field accepted", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/v1/captures", strings.NewReader(`{"interface":"lo"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(admin.Result().Cookies()[0])
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var job CaptureJob
	_ = json.Unmarshal(w.Body.Bytes(), &job)
	awaitCapture(t, c, job.ID)
	r = httptest.NewRequest("GET", "/api/v1/captures/"+job.ID+"/download", nil)
	r.AddCookie(admin.Result().Cookies()[0])
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/vnd.tcpdump.pcap" || !bytes.Equal(w.Body.Bytes(), pcapFixture(binary.LittleEndian, false, 64, 1)) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("binary download failed", w.Code)
	}
}
