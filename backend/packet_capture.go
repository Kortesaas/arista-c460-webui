package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	captureMaxSeconds = 120
	captureMaxBytes   = 8 << 20
	captureMaxJobs    = 3
	captureRetention  = 10 * time.Minute
)

type CaptureInput struct {
	Interface  string `json:"interface"`
	Protocol   string `json:"protocol"` // all, arp, icmp (v4 and v6), tcp, udp
	Host       string `json:"host"`     // numeric IP only; no DNS during capture
	Port       int    `json:"port"`
	Seconds    int    `json:"seconds"`
	MaxBytes   int64  `json:"maxBytes"`
	SnapLength int    `json:"snapLength"`
}

func normalizeCapture(v CaptureInput) (CaptureInput, error) {
	v.Interface, v.Host = strings.TrimSpace(v.Interface), strings.TrimSpace(v.Host)
	if v.Protocol == "" {
		v.Protocol = "all"
	}
	if v.Seconds == 0 {
		v.Seconds = 10
	}
	if v.MaxBytes == 0 {
		v.MaxBytes = 1 << 20
	}
	if v.SnapLength == 0 {
		v.SnapLength = 128
	}
	if v.Seconds < 1 || v.Seconds > captureMaxSeconds {
		return v, errors.New("duration must be 1–120 seconds")
	}
	if v.MaxBytes < 65536 || v.MaxBytes > captureMaxBytes {
		return v, errors.New("file size must be 65536–8388608 bytes")
	}
	if v.SnapLength < 64 || v.SnapLength > 4096 {
		return v, errors.New("packet length must be 64–4096 bytes")
	}
	switch v.Protocol {
	case "all", "arp", "icmp", "tcp", "udp":
	default:
		return v, errors.New("choose all, arp, icmp, tcp or udp")
	}
	if v.Host != "" {
		ip := net.ParseIP(v.Host)
		if ip == nil {
			return v, errors.New("host must be a numeric IPv4 or IPv6 address")
		}
		v.Host = ip.String()
	}
	if v.Port < 0 || v.Port > 65535 || v.Port != 0 && (v.Protocol == "arp" || v.Protocol == "icmp") {
		return v, errors.New("port must be 1–65535 and applies only to all, TCP or UDP traffic")
	}
	return v, nil
}

// Only structured, validated filter terms enter argv. No shell text, monitor
// mode, promiscuous mode, files, post-capture commands or tcpdump options from users.
func captureArgs(v CaptureInput) []string {
	args := []string{"-p", "-nn", "-U", "-i", v.Interface, "-s", strconv.Itoa(v.SnapLength), "-w", "-"}
	terms := []string{}
	if v.Protocol == "icmp" {
		terms = append(terms, "(icmp or icmp6)")
	} else if v.Protocol != "all" {
		terms = append(terms, v.Protocol)
	}
	if v.Host != "" {
		terms = append(terms, "host "+v.Host)
	}
	if v.Port != 0 {
		terms = append(terms, "port "+strconv.Itoa(v.Port))
	}
	if len(terms) > 0 {
		args = append(args, strings.Join(terms, " and "))
	}
	return args
}

type CaptureInterface struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Network string `json:"network,omitempty"`
	Up      bool   `json:"up"`
}

var captureInterfaceName = regexp.MustCompile(`^(lo|eth[0-9]+(?:\.[0-9]+)?|ath[0-9]{2,3}(?:\.[0-9]+)?|br[0-9]+(?:\.[0-9]+)?)$`)
var captureID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func captureInterfaces() []CaptureInterface {
	list := []CaptureInterface{{Name: "any", Kind: "All interfaces", Up: true}}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if !captureInterfaceName.MatchString(iface.Name) {
			continue
		}
		kind := "Ethernet"
		switch {
		case iface.Name == "lo":
			kind = "Loopback"
		case strings.HasPrefix(iface.Name, "ath"):
			kind = "Wireless data"
		case strings.HasPrefix(iface.Name, "br"):
			kind = "Bridge"
		}
		list = append(list, CaptureInterface{Name: iface.Name, Kind: kind, Up: iface.Flags&net.FlagUp != 0})
	}
	sort.Slice(list[1:], func(i, j int) bool { return list[i+1].Name < list[j+1].Name })
	return list
}

type CaptureJob struct {
	ID         string       `json:"id"`
	Input      CaptureInput `json:"input"`
	State      string       `json:"state"`            // running, completed, stopped, failed
	Reason     string       `json:"reason,omitempty"` // time-limit, size-limit, cancelled, exited, error
	StartedAt  time.Time    `json:"startedAt"`
	FinishedAt *time.Time   `json:"finishedAt,omitempty"`
	ExpiresAt  *time.Time   `json:"expiresAt,omitempty"`
	Bytes      int64        `json:"bytes"`
	Packets    int64        `json:"packets"`
	Download   bool         `json:"download"`
	Error      string       `json:"error,omitempty"`
}

type captureTask struct {
	CaptureJob
	cancel context.CancelFunc
	path   string
}
type captureRunner func(context.Context, []string, io.Writer) error

type PacketCaptures struct {
	mu         sync.Mutex
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	dir        string
	initErr    error
	jobs       map[string]*captureTask
	run        captureRunner
	interfaces func() []CaptureInterface
	available  func() bool
	closed     bool
}

func NewPacketCaptures(parent context.Context, dir string) *PacketCaptures {
	ctx, cancel := context.WithCancel(parent)
	c := &PacketCaptures{ctx: ctx, cancel: cancel, dir: dir, jobs: map[string]*captureTask{}, run: runPacketCapture, interfaces: captureInterfaces, available: func() bool {
		info, err := os.Stat("/sbin/tcpdump")
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
	}}
	c.initErr = os.MkdirAll(dir, 0700)
	if c.initErr == nil {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			c.initErr = errors.New("invalid capture directory")
		} else {
			c.initErr = os.Chmod(dir, 0700)
		}
	}
	if c.initErr == nil {
		// Captures are ephemeral. Remove only our named artifacts after a crash
		// or restart; never recurse into an arbitrary directory or symlink.
		entries, err := os.ReadDir(dir)
		if err != nil {
			c.initErr = err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".pcap") && captureID.MatchString(strings.TrimSuffix(entry.Name(), ".pcap")) {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					c.initErr = err
				}
			}
		}
	}
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				c.mu.Lock()
				c.pruneLocked(now, false)
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func runPacketCapture(ctx context.Context, args []string, out io.Writer) error {
	cmd := vendorShell(ctx, append([]string{"/sbin/tcpdump"}, args...)...)
	protectCaptureProcess(cmd)
	cmd.Stdout = out
	// tcpdump prints only statistics/errors here. Never export unstructured
	// output in the API or support bundle.
	cmd.Stderr = &cappedOutput{limit: 2048}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

func (c *PacketCaptures) supported() bool { return c != nil && c.initErr == nil && c.available() }

func (c *PacketCaptures) pruneLocked(now time.Time, space bool) {
	var oldest *captureTask
	for id, task := range c.jobs {
		if task.State == "running" {
			continue
		}
		if task.ExpiresAt != nil && !now.Before(*task.ExpiresAt) {
			if os.Remove(task.path) == nil || !fileExists(task.path) {
				delete(c.jobs, id)
			}
		} else if oldest == nil || task.StartedAt.Before(oldest.StartedAt) {
			oldest = task
		}
	}
	if space && len(c.jobs) >= captureMaxJobs && oldest != nil {
		if os.Remove(oldest.path) == nil || !fileExists(oldest.path) {
			delete(c.jobs, oldest.ID)
		}
	}
}

func fileExists(path string) bool { _, err := os.Stat(path); return !errors.Is(err, os.ErrNotExist) }

func (c *PacketCaptures) list() []CaptureJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now(), false)
	jobs := make([]CaptureJob, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, job.CaptureJob)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].StartedAt.After(jobs[j].StartedAt) })
	return jobs
}

var errCaptureBusy = errors.New("another packet capture is running")
var errCaptureUnavailable = errors.New("packet capture is unavailable on this AP")

func (c *PacketCaptures) start(input CaptureInput) (CaptureJob, error) {
	v, err := normalizeCapture(input)
	if err != nil {
		return CaptureJob{}, err
	}
	if !c.supported() {
		return CaptureJob{}, errCaptureUnavailable
	}
	valid := false
	for _, iface := range c.interfaces() {
		if iface.Name == v.Interface && iface.Up {
			valid = true
		}
	}
	if !valid {
		return CaptureJob{}, errors.New("choose an available, enabled capture interface")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return CaptureJob{}, errCaptureUnavailable
	}
	for _, job := range c.jobs {
		if job.State == "running" {
			return CaptureJob{}, errCaptureBusy
		}
	}
	c.pruneLocked(time.Now(), true)
	if len(c.jobs) >= captureMaxJobs {
		return CaptureJob{}, errors.New("could not clean up old captures")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return CaptureJob{}, errors.New("could not allocate capture ID")
	}
	id := hex.EncodeToString(buf)
	path := filepath.Join(c.dir, id+".pcap")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return CaptureJob{}, errors.New("could not create capture file")
	}
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(v.Seconds)*time.Second)
	job := &captureTask{CaptureJob: CaptureJob{ID: id, Input: v, State: "running", StartedAt: time.Now().UTC()}, cancel: cancel, path: path}
	c.jobs[id] = job
	c.wg.Add(1)
	go c.execute(ctx, job, file)
	return job.CaptureJob, nil
}

func (c *PacketCaptures) execute(ctx context.Context, task *captureTask, file *os.File) {
	defer c.wg.Done()
	defer task.cancel()
	sink := &pcapSink{out: file, limit: task.Input.MaxBytes, maxSnap: uint32(task.Input.SnapLength), progress: func(bytes, packets int64, limit bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		task.Bytes, task.Packets = bytes, packets
		if limit && task.Reason == "" {
			task.Reason = "size-limit"
			task.cancel()
		}
	}}
	err := c.run(ctx, captureArgs(task.Input), sink)
	closeErr := file.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	now, expiry := time.Now().UTC(), time.Now().UTC().Add(captureRetention)
	task.FinishedAt, task.ExpiresAt = &now, &expiry
	task.State = "completed"
	switch {
	case sink.err != nil && !errors.Is(sink.err, errCaptureSize), closeErr != nil:
		task.State, task.Reason, task.Error = "failed", "error", "Could not write a valid packet capture."
	case task.Reason == "size-limit":
	case task.Reason == "cancelled" || errors.Is(ctx.Err(), context.Canceled):
		task.State, task.Reason = "stopped", "cancelled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		task.Reason = "time-limit"
	case err != nil:
		task.State, task.Reason, task.Error = "failed", "error", "Packet capture failed. The interface may not support capture."
	default:
		task.Reason = "exited"
	}
	// Only complete records have been written; an interrupted partial record
	// stays in the sink buffer. Header-only captures are valid empty PCAPs.
	task.Download = sink.header && task.State != "failed"
	if !task.Download {
		_ = os.Remove(task.path)
	}
}

func (c *PacketCaptures) get(id string) (CaptureJob, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now(), false)
	job, ok := c.jobs[id]
	if !ok {
		return CaptureJob{}, false
	}
	return job.CaptureJob, true
}

func (c *PacketCaptures) stop(id string) (CaptureJob, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job, ok := c.jobs[id]
	if !ok {
		return CaptureJob{}, false
	}
	if job.State == "running" && job.Reason == "" {
		job.Reason = "cancelled"
		job.cancel()
	}
	return job.CaptureJob, true
}

func (c *PacketCaptures) remove(id string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job, ok := c.jobs[id]
	if !ok {
		return false, nil
	}
	if job.State == "running" {
		return true, errors.New("stop the capture before deleting it")
	}
	if err := os.Remove(job.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return true, errors.New("could not delete capture file")
	}
	delete(c.jobs, id)
	return true, nil
}

func (c *PacketCaptures) open(id string) (*os.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now(), false)
	job, ok := c.jobs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !job.Download || job.State == "running" {
		return nil, errors.New("capture is not ready to download")
	}
	return os.Open(job.path)
}

func (c *PacketCaptures) Close() {
	c.mu.Lock()
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, job := range c.jobs {
		_ = os.Remove(job.path)
		delete(c.jobs, id)
	}
}

var errCaptureSize = errors.New("capture size limit reached")

// Streaming classic-PCAP framing caps disk/RAM usage at the exact byte limit
// without cutting a packet in half. Both endian variants and nanosecond PCAP
// are accepted. tcpdump's -w output is classic PCAP, not pcapng.
type pcapSink struct {
	out            io.Writer
	limit          int64
	maxSnap        uint32
	buf            []byte
	header         bool
	order          binary.ByteOrder
	snap           uint32
	bytes, packets int64
	err            error
	progress       func(int64, int64, bool)
}

func (s *pcapSink) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n := len(p)
	for len(p) > 0 {
		need := 24
		if s.header {
			need = 16
			if len(s.buf) >= 16 {
				length := s.order.Uint32(s.buf[8:12])
				if length > s.snap || length > s.maxSnap || length > s.order.Uint32(s.buf[12:16]) {
					s.err = errors.New("invalid PCAP record length")
					return n - len(p), s.err
				}
				need += int(length)
			}
		}
		if len(s.buf) < need {
			take := min(need-len(s.buf), len(p))
			s.buf = append(s.buf, p[:take]...)
			p = p[take:]
			if len(s.buf) < need {
				break
			}
			if s.header && need == 16 && s.order.Uint32(s.buf[8:12]) != 0 {
				continue
			}
		}
		if !s.header {
			switch hex.EncodeToString(s.buf[:4]) {
			case "d4c3b2a1", "4d3cb2a1":
				s.order = binary.LittleEndian
			case "a1b2c3d4", "a1b23c4d":
				s.order = binary.BigEndian
			default:
				s.err = errors.New("invalid PCAP header")
			}
			if s.err == nil {
				s.snap = s.order.Uint32(s.buf[16:20])
				if s.order.Uint16(s.buf[4:6]) != 2 || s.order.Uint16(s.buf[6:8]) != 4 || s.snap == 0 || s.snap > s.maxSnap {
					s.err = errors.New("invalid PCAP format")
				}
			}
			if s.err != nil {
				return n - len(p), s.err
			}
		}
		if s.bytes+int64(len(s.buf)) > s.limit {
			s.err = errCaptureSize
			s.progress(s.bytes, s.packets, true)
			return n - len(p), s.err
		}
		written, err := s.out.Write(s.buf)
		if err != nil || written != len(s.buf) {
			if err == nil {
				err = io.ErrShortWrite
			}
			s.err = err
			return n - len(p), err
		}
		s.bytes += int64(written)
		if s.header {
			s.packets++
		} else {
			s.header = true
		}
		s.buf = s.buf[:0]
		hit := s.bytes == s.limit
		s.progress(s.bytes, s.packets, hit)
		if hit {
			s.err = errCaptureSize
			return n - len(p), s.err
		}
	}
	return n, nil
}

type CaptureStatus struct {
	Supported        bool               `json:"supported"`
	Interfaces       []CaptureInterface `json:"interfaces"`
	Jobs             []CaptureJob       `json:"jobs"`
	MaxSeconds       int                `json:"maxSeconds"`
	MaxBytes         int64              `json:"maxBytes"`
	MaxJobs          int                `json:"maxJobs"`
	RetentionSeconds int                `json:"retentionSeconds"`
}

func (a *API) listCaptures(w http.ResponseWriter, r *http.Request) {
	status := CaptureStatus{Supported: a.captures.supported(), Interfaces: []CaptureInterface{}, Jobs: []CaptureJob{}, MaxSeconds: captureMaxSeconds, MaxBytes: captureMaxBytes, MaxJobs: captureMaxJobs, RetentionSeconds: int(captureRetention / time.Second)}
	if a.captures != nil {
		status.Interfaces, status.Jobs = a.captures.interfaces(), a.captures.list()
		networks := interfaceNetworks("/sys/class/net", a.snapshot())
		for i := range status.Interfaces {
			status.Interfaces[i].Network = networks[strings.Split(status.Interfaces[i].Name, ".")[0]]
		}
	}
	reply(w, 200, status)
}

func (a *API) startCapture(w http.ResponseWriter, r *http.Request) {
	var input CaptureInput
	if !decode(w, r, &input) {
		return
	}
	if a.captures == nil {
		fail(w, 503, errCaptureUnavailable.Error())
		return
	}
	job, err := a.captures.start(input)
	if err != nil {
		code := 400
		if errors.Is(err, errCaptureBusy) {
			code = 409
		}
		if errors.Is(err, errCaptureUnavailable) {
			code = 503
		}
		fail(w, code, err.Error())
		return
	}
	reply(w, 202, job)
}

func (a *API) getCapture(w http.ResponseWriter, r *http.Request) {
	if a.captures != nil {
		if job, ok := a.captures.get(r.PathValue("id")); ok {
			reply(w, 200, job)
			return
		}
	}
	fail(w, 404, "capture not found or expired")
}

func (a *API) stopCapture(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	if a.captures != nil {
		if job, ok := a.captures.stop(r.PathValue("id")); ok {
			reply(w, 200, job)
			return
		}
	}
	fail(w, 404, "capture not found or expired")
}

func (a *API) deleteCapture(w http.ResponseWriter, r *http.Request) {
	if a.captures != nil {
		found, err := a.captures.remove(r.PathValue("id"))
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		if found {
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, "capture not found or expired")
}

func (a *API) downloadCapture(w http.ResponseWriter, r *http.Request) {
	if a.captures == nil {
		fail(w, 404, "capture not found or expired")
		return
	}
	file, err := a.captures.open(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		fail(w, 404, "capture not found or expired")
		return
	}
	if err != nil {
		fail(w, 409, "capture is not ready to download")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, 500, "could not read capture file")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="capture-%s.pcap"`, r.PathValue("id")))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "capture.pcap", info.ModTime(), file)
}
