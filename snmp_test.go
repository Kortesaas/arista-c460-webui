package main

import (
	"bytes"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func testSNMPState() APState {
	return APState{
		Device: Device{Hostname: "AP-1", SiteName: "Hall", Model: "C-460", Firmware: "18.2.0-32", UptimeSeconds: 3600},
		Interfaces: []Interface{
			{Name: "eth0", Up: true, Speed: "2.5 Gbit/s", MAC: "E0:1C:A7:00:00:01", InOctets: 5_000_000_000, OutOctets: 42, Port: 2, Role: "uplink"},
			{Name: "eth1", Speed: "10 Gbit/s", MAC: "E0:1C:A7:00:00:02", Port: 1, Role: "backup"},
		},
	}
}

func testSNMPAgent(t *testing.T, settings SNMPSettings) *SNMPAgent {
	t.Helper()
	cfg := &Config{SNMP: settings}
	return NewSNMPAgent(cfg, testSNMPState)
}

func snmpRequest(version int64, community string, pdu byte, f1, f2 int64, oids ...OID) []byte {
	var varbinds [][]byte
	for _, o := range oids {
		varbinds = append(varbinds, berSeq(berSequence, berOID(o), berTLV(berNull, nil)))
	}
	return berSeq(berSequence, berInt(version), berString(community),
		berSeq(pdu, berInt(77), berInt(f1), berInt(f2), berSeq(berSequence, varbinds...)))
}

type snmpReply struct {
	status, index int64
	oids          []OID
	values        [][]byte
}

func parseReply(t *testing.T, packet []byte) snmpReply {
	t.Helper()
	outer := berReader{packet}
	msg, err := outer.expect(berSequence)
	if err != nil {
		t.Fatal(err)
	}
	r := berReader{msg}
	_, _ = r.integer()
	_, _ = r.expect(berOctetString)
	pdu, err := r.expect(pduResponse)
	if err != nil {
		t.Fatal(err)
	}
	p := berReader{pdu}
	if id, _ := p.integer(); id != 77 {
		t.Fatalf("request id %d", id)
	}
	var out snmpReply
	out.status, _ = p.integer()
	out.index, _ = p.integer()
	list, _ := p.expect(berSequence)
	for lr := (berReader{list}); len(lr.b) > 0; {
		vb, _ := lr.expect(berSequence)
		br := berReader{vb}
		ob, _ := br.expect(berObjectID)
		oid, err := parseOID(ob)
		if err != nil {
			t.Fatal(err)
		}
		_, _, raw, _ := br.next()
		out.oids = append(out.oids, oid)
		out.values = append(out.values, raw)
	}
	return out
}

func TestBEREncodingRoundTrip(t *testing.T) {
	for _, s := range []string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.4.1.8072.3.2.10", "1.3.6.1.2.1.31.1.1.1.6.4294967295", "2.999.1"} {
		o := mustOID(s)
		r := berReader{berOID(o)}
		c, err := r.expect(berObjectID)
		if err != nil {
			t.Fatal(err)
		}
		back, err := parseOID(c)
		if err != nil || back.String() != s {
			t.Fatalf("%s -> %v (%v)", s, back, err)
		}
	}
	for _, v := range []int64{0, 1, 127, 128, 255, 256, -1, -128, -129, 1 << 40} {
		r := berReader{berInt(v)}
		if got, err := r.integer(); err != nil || got != v {
			t.Fatalf("%d -> %d (%v)", v, got, err)
		}
	}
	if got := berUint(berCounter32, 0xffffffff); !bytes.Equal(got, []byte{0x41, 5, 0, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("counter32 %x", got)
	}
}

func TestSNMPAgentRequests(t *testing.T) {
	agent := testSNMPAgent(t, SNMPSettings{Enabled: true, Community: "monitor", Location: "Rack 2"})

	if agent.handle(snmpRequest(1, "public", pduGet, 0, 0, oidSystem.Append(5, 0))) != nil {
		t.Fatal("wrong community answered")
	}
	if agent.handle([]byte{0x30, 0x84, 0xff}) != nil {
		t.Fatal("garbage answered")
	}

	got := parseReply(t, agent.handle(snmpRequest(1, "monitor", pduGet, 0, 0, oidSystem.Append(5, 0), oidSystem.Append(6, 0), oidSystem.Append(9, 0))))
	if got.status != 0 || !bytes.Equal(got.values[0], berString("Hall")) || !bytes.Equal(got.values[1], berString("Rack 2")) || got.values[2][0] != berNoSuchObject {
		t.Fatalf("get: %+v", got)
	}

	// ifIndex follows the physical socket, not the kernel name.
	got = parseReply(t, agent.handle(snmpRequest(1, "monitor", pduGetNext, 0, 0, oidIfXEntry.Append(1))))
	if got.oids[0].Compare(oidIfXEntry.Append(1, 1)) != 0 || !bytes.Equal(got.values[0], berString("ETH 1")) {
		t.Fatalf("getnext: %v %x", got.oids, got.values)
	}

	// SNMPv1 cannot carry Counter64, so a v1 walk skips ifHCInOctets.
	got = parseReply(t, agent.handle(snmpRequest(0, "monitor", pduGetNext, 0, 0, oidIfXEntry.Append(1, 2))))
	if got.oids[0].Compare(oidIfXEntry.Append(15, 1)) != 0 {
		t.Fatalf("v1 getnext: %v", got.oids)
	}
	got = parseReply(t, agent.handle(snmpRequest(1, "monitor", pduGet, 0, 0, oidIfXEntry.Append(6, 2))))
	if !bytes.Equal(got.values[0], berUint(berCounter64, 5_000_000_000)) {
		t.Fatalf("hc counter %x", got.values[0])
	}
	got = parseReply(t, agent.handle(snmpRequest(1, "monitor", pduGet, 0, 0, oidIfEntry.Append(10, 2))))
	if !bytes.Equal(got.values[0], berUint(berCounter32, 5_000_000_000&0xffffffff)) {
		t.Fatalf("counter32 wrap %x", got.values[0])
	}

	got = parseReply(t, agent.handle(snmpRequest(1, "monitor", pduSet, 0, 0, oidSystem.Append(5, 0))))
	if got.status != snmpNotWritable {
		t.Fatalf("set status %d", got.status)
	}

	// GetBulk ends with endOfMibView and stays within one datagram.
	reply := agent.handle(snmpRequest(1, "monitor", pduGetBulk, 0, 50, mustOID("1.3.6.1.2.1.31.1.1.1.18")))
	if len(reply) > snmpMaxPacket {
		t.Fatalf("bulk reply %d bytes", len(reply))
	}
	got = parseReply(t, reply)
	if len(got.oids) != 3 || got.values[2][0] != berEndOfMibView {
		t.Fatalf("bulk: %v", got.oids)
	}
	reply = agent.handle(snmpRequest(1, "monitor", pduGetBulk, 0, 50, mustOID("1.3.6.1")))
	if len(reply) > snmpMaxPacket || len(parseReply(t, reply).oids) < 30 {
		t.Fatalf("full bulk reply %d bytes", len(reply))
	}
}

func TestSNMPSettingsValidation(t *testing.T) {
	for _, bad := range []SNMPSettings{{Enabled: true}, {Enabled: true, Community: " public"}, {Enabled: true, Community: "a\nb"}, {Enabled: true, Community: "ok", Location: strings.Repeat("x", 300)}} {
		if bad.validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if (SNMPSettings{}).validate() != nil {
		t.Fatal("disabled settings rejected")
	}
}

// Interoperability check with net-snmp's command line tools when installed.
func TestSNMPAgentWithNetSNMP(t *testing.T) {
	walk, err := exec.LookPath("snmpbulkwalk")
	if err != nil {
		t.Skip("net-snmp tools not installed")
	}
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	addr := probe.LocalAddr().String()
	_ = probe.Close()
	agent := testSNMPAgent(t, SNMPSettings{Enabled: true, Community: "monitor", Listen: addr})
	if err := agent.Apply(); err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	time.Sleep(50 * time.Millisecond)

	out, err := exec.Command(walk, "-v2c", "-c", "monitor", "-On", "-Oe", "-Ox", "-t", "2", "-r", "0", "udp:"+addr, "1.3.6.1.2.1").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	text := string(out)
	// net-snmp formats values differently depending on the MIBs installed
	// locally, so match each line by OID and accept either rendering.
	lines := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if oid, value, ok := strings.Cut(line, " = "); ok && !strings.HasPrefix(value, "No more variables") {
			lines[oid] = strings.ToLower(value)
		}
	}
	for oid, accepted := range map[string][]string{
		".1.3.6.1.2.1.1.5.0":         {"hall"},
		".1.3.6.1.2.1.2.2.1.8.2":     {"integer: 1", "up(1)"},
		".1.3.6.1.2.1.31.1.1.1.6.2":  {"counter64: 5000000000"},
		".1.3.6.1.2.1.31.1.1.1.15.2": {"gauge32: 2500"},
		".1.3.6.1.2.1.2.2.1.6.1":     {"e0:1c:a7:0:0:2", "e0 1c a7 00 00 02"},
		".1.3.6.1.2.1.31.1.1.1.18.2": {"uplink"},
	} {
		found := false
		for _, a := range accepted {
			found = found || strings.Contains(lines[oid], a)
		}
		if !found {
			t.Errorf("%s = %q, want one of %q", oid, lines[oid], accepted)
		}
	}
	if strings.Contains(text, "Error") || strings.Contains(text, "OID not increasing") {
		t.Errorf("walk reported a problem:\n%s", text)
	}
}

func TestSNMPDownPortHasNoSpeed(t *testing.T) {
	view := buildSNMPView(testSNMPState(), SNMPSettings{}, 0)
	for _, v := range view {
		if v.oid.Compare(oidIfXEntry.Append(15, 1)) == 0 && !bytes.Equal(v.value, berUint(berGauge32, 0)) {
			t.Fatalf("down port speed %x", v.value)
		}
	}
}
