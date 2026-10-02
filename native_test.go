package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeHostValidation(t *testing.T) {
	for _, v := range []string{"192.168.99.1", "fd00::1", "time.nist.gov", "ntp", "ntp.example.org."} {
		if !validHost(v) {
			t.Errorf("rejected %q", v)
		}
	}
	for _, v := range []string{"", "-p", "0.0.0.0", "224.0.0.1", "999.1.1.1", "127.1", "::", "a;b", "a\nb", "$(reboot)", "https://ntp.example.org", "a b", "ntp:123", "fe80::1%eth0"} {
		if validHost(v) {
			t.Errorf("accepted %q", v)
		}
	}
	if err := (TimeInput{Primary: "ntp.example", Secondary: "NTP.EXAMPLE"}).validate(); err == nil {
		t.Fatal("accepted duplicate NTP servers")
	}
	for _, v := range []DiagnosticInput{{"shell", "127.0.0.1"}, {"ping", "-c"}, {"dns", "a;reboot"}} {
		if _, err := diagnosticArgs(v); err == nil {
			t.Errorf("accepted %+v", v)
		}
	}
	args, err := diagnosticArgs(DiagnosticInput{"ping", "::1"})
	if err != nil || args[0] != "ping6" {
		t.Fatal(args, err)
	}
}

func TestNativeTimeTransaction(t *testing.T) {
	for _, mode := range []string{"success", "encryption fails", "restart fails", "concurrent native writer", "restore desired"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			sensor, desired := filepath.Join(dir, "sensor.conf"), filepath.Join(dir, "time-settings.json")
			before := []byte("[Sensor]\nntpserver = old.example\nsecntpserver =\noc_enabled = 1\napi_user = keep-this\n")
			os.WriteFile(sensor, before, 0600)
			os.WriteFile(sensor+".enc", []byte("old ciphertext"), 0600)
			restarts := 0
			backend := nativeTimeBackend{sensor: sensor, desired: desired, restart: func(context.Context) error {
				restarts++
				if mode == "restart fails" && restarts == 1 {
					return errors.New("service failed")
				}
				return nil
			}, encrypt: func(ctx context.Context, plain, enc string) error {
				if mode == "encryption fails" {
					return errors.New("TPM failed")
				}
				if mode == "concurrent native writer" {
					return os.WriteFile(sensor, []byte("other writer"), 0600)
				}
				return os.WriteFile(enc, []byte("new ciphertext"), 0600)
			}}
			input := TimeInput{Primary: "new.example", Secondary: "192.168.99.1"}
			err := backend.save(context.Background(), input, mode != "restore desired")
			data, _ := os.ReadFile(sensor)
			cipher, _ := os.ReadFile(sensor + ".enc")
			_, savedErr := os.Stat(desired)
			if mode == "success" || mode == "restore desired" {
				if err != nil {
					t.Fatal(err)
				}
				if nativeField(data, "ntpserver") != input.Primary || nativeField(data, "secntpserver") != input.Secondary || !strings.Contains(string(data), "api_user = keep-this") {
					t.Fatalf("native fields lost: %s", data)
				}
				if restarts != 1 || string(cipher) != "new ciphertext" {
					t.Fatalf("not applied: restart=%d cipher=%s", restarts, cipher)
				}
				if mode == "success" && savedErr != nil {
					t.Fatal("desired settings not persisted")
				}
				if mode == "restore desired" && !os.IsNotExist(savedErr) {
					t.Fatal("restore modified desired settings")
				}
				// No-op must not restart NTP or alter the encrypted configuration.
				if err = backend.save(context.Background(), input, false); err != nil || restarts != 1 {
					t.Fatal("no-op restarted NTP", err)
				}
			} else {
				if err == nil {
					t.Fatal("failure reported success")
				}
				if mode == "concurrent native writer" {
					if string(data) != "other writer" {
						t.Fatal("overwrote another writer")
					}
				} else if string(data) != string(before) {
					t.Fatal("native config not rolled back")
				}
				if string(cipher) != "old ciphertext" || !os.IsNotExist(savedErr) {
					t.Fatal("encrypted/desired copies not restored")
				}
			}
		})
	}
}

func TestHostapdSocketProtocol(t *testing.T) {
	dir, err := os.MkdirTemp("", "c460-hp-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "ath00"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 2048)
		n, addr, e := socket.ReadFromUnix(buf)
		if e != nil {
			done <- e.Error()
			return
		}
		done <- string(buf[:n])
		socket.WriteToUnix([]byte("state=ENABLED\nssid[0]=Test\ndtim_period=2\nsecret=never-expose\n"), addr)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := hostapdCommand(ctx, dir, "ath00", "STATUS")
	if err != nil {
		t.Fatal(err)
	}
	if cmd := <-done; cmd != "STATUS" {
		t.Fatalf("wrong command: %s", cmd)
	}
	props := selectProperties(hostapdProperties(out), statusFields)
	if props["dtim_period"] != "2" || props["secret"] != "" {
		t.Fatal("bad status filtering", props)
	}
	if _, err = hostapdCommand(ctx, dir, "../../etc/passwd", "STATUS"); err == nil {
		t.Fatal("accepted path traversal")
	}
	cancel()
	if _, err = hostapdCommand(ctx, dir, "ath00", "STATUS"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestNewEndpointsRequireSession(t *testing.T) {
	a := &API{auth: NewAuth(filepath.Join(t.TempDir(), "auth.json"))}
	mux := http.NewServeMux()
	a.Register(mux)
	for _, route := range []struct{ method, path string }{{"GET", "/api/time"}, {"PUT", "/api/time"}, {"GET", "/api/wireless-status"}, {"POST", "/api/diagnostics"}, {"GET", "/api/clients/00:11:22:33:44:55/details"}, {"POST", "/api/clients/00:11:22:33:44:55/reconnect"}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader("{}")))
		if w.Code != 401 {
			t.Errorf("%+v: %d", route, w.Code)
		}
	}
}

func TestNativeClientActions(t *testing.T) {
	for _, scenario := range []string{"details", "reconnect", "gone", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "c460-sta-test-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "ath11"), Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer socket.Close()
			mac := "02:11:22:33:44:55"
			done := make(chan error, 1)
			go func() {
				socket.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 2048)
				n, addr, e := socket.ReadFromUnix(buf)
				if e != nil {
					done <- e
					return
				}
				if string(buf[:n]) != "STA "+mac {
					done <- errors.New("wrong station query")
					return
				}
				if scenario == "gone" {
					socket.WriteToUnix([]byte("FAIL\n"), addr)
					done <- nil
					return
				}
				socket.WriteToUnix([]byte(mac+"\nflags=[AUTH][ASSOC][AUTHORIZED]\naid=1\nconnected_time=240\nsecret=hidden\n"), addr)
				if scenario == "details" {
					done <- nil
					return
				}
				n, addr, e = socket.ReadFromUnix(buf)
				if e != nil {
					done <- e
					return
				}
				if string(buf[:n]) != "DEAUTHENTICATE "+mac+" reason=2" {
					done <- errors.New("wrong reconnect command")
					return
				}
				response := "OK\n"
				if scenario == "rejected" {
					response = "FAIL\n"
				}
				socket.WriteToUnix([]byte(response), addr)
				done <- nil
			}()
			a := &API{wirelessDir: dir}
			w := httptest.NewRecorder()
			method := "POST"
			if scenario == "details" {
				method = "GET"
			}
			r := httptest.NewRequest(method, "/api/clients/"+mac+"/details", strings.NewReader("{}"))
			r.SetPathValue("mac", mac)
			a.nativeClient(w, r, scenario != "details")
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			expected := 200
			if scenario == "gone" {
				expected = 404
			} else if scenario == "rejected" {
				expected = 502
			}
			if w.Code != expected {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if scenario == "details" && (!strings.Contains(w.Body.String(), `"interface":"ath11"`) || strings.Contains(w.Body.String(), "hidden")) {
				t.Fatal("client details not filtered", w.Body.String())
			}
		})
	}
}

func TestStaticClientARPDiscovery(t *testing.T) {
	arp := parseARPAddresses(`IP address       HW type     Flags       HW address            Mask     Device
192.168.99.200   0x1         0x2         42:1d:4a:30:4a:01     *        br0
192.168.99.1     0x1         0x0         00:00:00:00:00:00     *        br0
192.168.10.2     0x1         0x2         02:11:22:33:44:55     *        br0.10
192.168.20.2     0x1         0x2         02:11:22:33:44:55     *        br0.20
bad-address     0x1         0x2         02:11:22:33:44:66     *        br0
`)
	if len(arp) != 1 || arp["42:1d:4a:30:4a:01"] != "192.168.99.200" {
		t.Fatal("invalid or ambiguous ARP entry included", arp)
	}
	clients := []Client{{MAC: "42:1D:4A:30:4A:01"}, {MAC: "42:1d:4a:30:4a:01", IPv4: "192.168.99.201"}, {MAC: "02:11:22:33:44:55"}}
	supplementClientAddresses(clients, arp)
	if clients[0].IPv4 != "192.168.99.200" || clients[0].IPv4Source != "arp" {
		t.Fatal("static address missing", clients)
	}
	if clients[1].IPv4 != "192.168.99.201" || clients[1].IPv4Source != "" || clients[2].IPv4 != "" {
		t.Fatal("overwrote telemetry or guessed an address", clients)
	}
}
