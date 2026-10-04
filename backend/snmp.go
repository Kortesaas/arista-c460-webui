package main

// Read-only SNMP v1/v2c agent so network monitors can poll the AP like any
// other device: MIB-II system group, ifTable and ifXTable for the two
// Ethernet sockets. Writes are always refused.

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type SNMPSettings struct {
	Enabled   bool   `json:"enabled"`
	Community string `json:"community"`
	Location  string `json:"location"`
	Contact   string `json:"contact"`
	Listen    string `json:"listen,omitempty"` // default ":161"
}

func (s SNMPSettings) validate() error {
	if !s.Enabled {
		return nil
	}
	if s.Community == "" || len(s.Community) > 64 || strings.TrimSpace(s.Community) != s.Community {
		return errors.New("the community needs 1–64 characters without leading or trailing spaces")
	}
	for _, field := range []string{s.Community, s.Location, s.Contact} {
		if !utf8.ValidString(field) || strings.ContainsAny(field, "\x00\r\n") || len(field) > 255 {
			return errors.New("community, location and contact must be single lines of at most 255 bytes")
		}
	}
	return nil
}

func (c *Config) SNMPSettings() SNMPSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SNMP
}

func (c *Config) SetSNMP(s SNMPSettings) error {
	if err := s.validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.SNMP
	s.Listen = previous.Listen // not editable from the browser
	c.SNMP = s
	if err := c.saveLocked(); err != nil {
		c.SNMP = previous
		return err
	}
	return nil
}

const (
	snmpMaxPacket   = 1472 // one unfragmented Ethernet frame
	snmpMaxVarbinds = 64
	snmpMaxRepeats  = 50
	snmpRatePerSec  = 200

	snmpNoError     = 0
	snmpTooBig      = 1
	snmpNoSuchName  = 2
	snmpReadOnly    = 4
	snmpNotWritable = 17
)

var (
	oidSystem   = mustOID("1.3.6.1.2.1.1")
	oidIfNumber = mustOID("1.3.6.1.2.1.2.1.0")
	oidIfEntry  = mustOID("1.3.6.1.2.1.2.2.1")
	oidIfXEntry = mustOID("1.3.6.1.2.1.31.1.1.1")
	// Generic Linux device, as reported by most embedded net-snmp agents.
	oidLinuxDevice = mustOID("1.3.6.1.4.1.8072.3.2.10")
)

type snmpVar struct {
	oid   OID
	value []byte // complete BER element
	wide  bool   // Counter64, not representable in SNMPv1
}

type SNMPAgent struct {
	cfg     *Config
	state   func() APState
	started time.Time

	mu   sync.Mutex
	conn net.PacketConn
	addr string

	viewMu   sync.Mutex
	view     []snmpVar
	viewTime time.Time

	rateMu     sync.Mutex
	tokens     float64
	tokensTime time.Time
}

func NewSNMPAgent(cfg *Config, state func() APState) *SNMPAgent {
	return &SNMPAgent{cfg: cfg, state: state, started: time.Now()}
}

// Apply opens, moves or closes the UDP listener to match the configuration.
func (s *SNMPAgent) Apply() error {
	settings := s.cfg.SNMPSettings()
	addr := settings.Listen
	if addr == "" {
		addr = ":161"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil && (!settings.Enabled || addr != s.addr) {
		_ = s.conn.Close()
		s.conn = nil
		log.Printf("snmp stopped")
	}
	if !settings.Enabled || s.conn != nil {
		return nil
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("snmp listen %s: %w", addr, err)
	}
	s.conn, s.addr = conn, addr
	log.Printf("snmp listening on udp %s (read-only)", addr)
	go s.serve(conn)
	return nil
}

func (s *SNMPAgent) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *SNMPAgent) serve(conn net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if !s.allow() {
			continue
		}
		if reply := s.handle(buf[:n]); reply != nil {
			_, _ = conn.WriteTo(reply, peer)
		}
	}
}

// allow is a token bucket that bounds the reply rate.
func (s *SNMPAgent) allow() bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	s.tokens += now.Sub(s.tokensTime).Seconds() * snmpRatePerSec
	s.tokensTime = now
	if s.tokens > snmpRatePerSec {
		s.tokens = snmpRatePerSec
	}
	if s.tokens < 1 {
		return false
	}
	s.tokens--
	return true
}

type snmpBinding struct {
	oid OID
	raw []byte // value as received, echoed back in v1 error replies
}

// handle answers one request datagram, or returns nil to stay silent
// (malformed packets, wrong community, unsupported versions).
func (s *SNMPAgent) handle(packet []byte) []byte {
	settings := s.cfg.SNMPSettings()
	if !settings.Enabled || settings.Community == "" {
		return nil
	}
	outer := berReader{packet}
	msg, err := outer.expect(berSequence)
	if err != nil {
		return nil
	}
	r := berReader{msg}
	version, err := r.integer()
	if err != nil || (version != 0 && version != 1) {
		return nil
	}
	community, err := r.expect(berOctetString)
	if err != nil || subtle.ConstantTimeCompare(community, []byte(settings.Community)) != 1 {
		return nil
	}
	pduType, pdu, _, err := r.next()
	if err != nil {
		return nil
	}
	p := berReader{pdu}
	requestID, err1 := p.integer()
	field1, err2 := p.integer()
	field2, err3 := p.integer()
	list, err4 := p.expect(berSequence)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return nil
	}
	var bindings []snmpBinding
	for lr := (berReader{list}); len(lr.b) > 0; {
		vb, err := lr.expect(berSequence)
		if err != nil || len(bindings) == snmpMaxVarbinds {
			return nil
		}
		br := berReader{vb}
		oidBytes, err := br.expect(berObjectID)
		if err != nil {
			return nil
		}
		oid, err := parseOID(oidBytes)
		if err != nil {
			return nil
		}
		_, _, raw, err := br.next()
		if err != nil {
			return nil
		}
		bindings = append(bindings, snmpBinding{oid: oid, raw: raw})
	}

	v1 := version == 0
	reply := func(status, index int, varbinds [][]byte) []byte {
		return berSeq(berSequence, berInt(version), berTLV(berOctetString, community),
			berSeq(pduResponse, berInt(requestID), berInt(int64(status)), berInt(int64(index)), berSeq(berSequence, varbinds...)))
	}
	echo := func(status, index int) []byte {
		varbinds := make([][]byte, len(bindings))
		for i, b := range bindings {
			varbinds[i] = berSeq(berSequence, berOID(b.oid), b.raw)
		}
		return reply(status, index, varbinds)
	}
	fits := func(varbinds [][]byte) bool { return len(reply(0, 0, varbinds)) <= snmpMaxPacket }
	tooBig := func() []byte {
		if v1 {
			return echo(snmpTooBig, 0)
		}
		return reply(snmpTooBig, 0, nil)
	}

	view := s.currentView()
	switch pduType {
	case pduGet:
		var out [][]byte
		for i, b := range bindings {
			value := snmpGet(view, b.oid, v1)
			if value == nil {
				return echo(snmpNoSuchName, i+1)
			}
			out = append(out, berSeq(berSequence, berOID(b.oid), value))
		}
		if !fits(out) {
			return tooBig()
		}
		return reply(snmpNoError, 0, out)

	case pduGetNext:
		var out [][]byte
		for i, b := range bindings {
			next, ok := snmpNext(view, b.oid, v1)
			if !ok {
				if v1 {
					return echo(snmpNoSuchName, i+1)
				}
				out = append(out, berSeq(berSequence, berOID(b.oid), berTLV(berEndOfMibView, nil)))
				continue
			}
			out = append(out, berSeq(berSequence, berOID(next.oid), next.value))
		}
		if !fits(out) {
			return tooBig()
		}
		return reply(snmpNoError, 0, out)

	case pduGetBulk:
		if v1 {
			return nil
		}
		nonRepeaters := int(min(max(field1, 0), int64(len(bindings))))
		repeats := int(min(max(field2, 0), snmpMaxRepeats))
		var out [][]byte
		step := func(oid OID) ([]byte, OID, bool) {
			next, ok := snmpNext(view, oid, false)
			if !ok {
				return berSeq(berSequence, berOID(oid), berTLV(berEndOfMibView, nil)), oid, false
			}
			return berSeq(berSequence, berOID(next.oid), next.value), next.oid, true
		}
		for _, b := range bindings[:nonRepeaters] {
			vb, _, _ := step(b.oid)
			out = append(out, vb)
		}
		if !fits(out) {
			return tooBig()
		}
		cursors := make([]OID, 0, len(bindings)-nonRepeaters)
		for _, b := range bindings[nonRepeaters:] {
			cursors = append(cursors, b.oid)
		}
	repetitions:
		for rep := 0; rep < repeats && len(cursors) > 0; rep++ {
			row := make([][]byte, 0, len(cursors))
			more := false
			for i, oid := range cursors {
				vb, next, ok := step(oid)
				row = append(row, vb)
				cursors[i] = next
				more = more || ok
			}
			// RFC 3416 allows truncating whole repetitions to fit.
			if !fits(append(out[:len(out):len(out)], row...)) {
				break repetitions
			}
			out = append(out, row...)
			if !more {
				break
			}
		}
		return reply(snmpNoError, 0, out)

	case pduSet:
		if v1 {
			return echo(snmpReadOnly, 1)
		}
		return echo(snmpNotWritable, 1)
	}
	return nil
}

func snmpFind(view []snmpVar, oid OID) int {
	return sort.Search(len(view), func(i int) bool { return view[i].oid.Compare(oid) >= 0 })
}

// snmpGet returns the encoded value, the v2c exception, or nil for a v1 error.
func snmpGet(view []snmpVar, oid OID, v1 bool) []byte {
	i := snmpFind(view, oid)
	if i < len(view) && view[i].oid.Compare(oid) == 0 && !(v1 && view[i].wide) {
		return view[i].value
	}
	if v1 {
		return nil
	}
	if i < len(view) && len(view[i].oid) > len(oid) && view[i].oid[:len(oid)].Compare(oid) == 0 {
		return berTLV(berNoSuchObject, nil) // oid names a table or column, not an instance
	}
	// An existing object (column) with a missing instance is noSuchInstance.
	for cut := len(oid) - 1; cut > 0; cut-- {
		j := snmpFind(view, oid[:cut])
		if j < len(view) && len(view[j].oid) == cut+1 && view[j].oid[:cut].Compare(oid[:cut]) == 0 {
			return berTLV(berNoSuchInstance, nil)
		}
	}
	return berTLV(berNoSuchObject, nil)
}

func snmpNext(view []snmpVar, oid OID, v1 bool) (snmpVar, bool) {
	i := snmpFind(view, oid)
	if i < len(view) && view[i].oid.Compare(oid) == 0 {
		i++
	}
	for ; i < len(view); i++ {
		if !(v1 && view[i].wide) {
			return view[i], true
		}
	}
	return snmpVar{}, false
}

// currentView caches the MIB for a second so a walk sees consistent counters.
func (s *SNMPAgent) currentView() []snmpVar {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.view == nil || time.Since(s.viewTime) > time.Second {
		s.view = buildSNMPView(s.state(), s.cfg.SNMPSettings(), time.Since(s.started))
		s.viewTime = time.Now()
	}
	return s.view
}

var speedPattern = regexp.MustCompile(`^([0-9.]+)\s*(M|G)bit/s$`)

func speedMbps(speed string) uint64 {
	m := speedPattern.FindStringSubmatch(speed)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v < 0 {
		return 0
	}
	if m[2] == "G" {
		v *= 1000
	}
	return uint64(v)
}

func macBytes(mac string) []byte {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return nil
	}
	return hw
}

type snmpInterface struct {
	index uint32
	Interface
}

// mbps is the link speed; the firmware keeps reporting a stale speed for
// sockets without link.
func (i snmpInterface) mbps() uint64 {
	if !i.Up {
		return 0
	}
	return speedMbps(i.Speed)
}

// snmpInterfaces numbers the Ethernet sockets by their physical port, so
// ifIndex 1 is always ETH 1 even when the firmware swaps eth0 and eth1.
func snmpInterfaces(list []Interface) []snmpInterface {
	out := make([]snmpInterface, 0, len(list))
	next := uint32(10)
	for _, iface := range list {
		index := uint32(iface.Port)
		if index == 0 {
			index = next
			next++
		}
		out = append(out, snmpInterface{index: index, Interface: iface})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].index < out[j].index })
	return out
}

func buildSNMPView(st APState, settings SNMPSettings, agentUptime time.Duration) []snmpVar {
	var view []snmpVar
	add := func(oid OID, value []byte) { view = append(view, snmpVar{oid: oid, value: value}) }
	gauge := func(v uint64) []byte { return berUint(berGauge32, min(v, 0xffffffff)) }
	counter := func(v float64) []byte { return berUint(berCounter32, uint64(max(v, 0))&0xffffffff) }

	dev := st.Device
	model := dev.Model
	if model == "" {
		model = "C-460"
	}
	descr := fmt.Sprintf("%s Wi-Fi access point", model)
	if dev.Firmware != "" {
		descr += ", firmware " + dev.Firmware
	}
	descr += ", arista-c460-webui " + version
	name := dev.SiteName
	if name == "" {
		name = dev.Hostname
	}
	uptime := agentUptime.Seconds()
	if dev.UptimeSeconds > 0 {
		uptime = dev.UptimeSeconds
	}
	add(oidSystem.Append(1, 0), berString(descr))
	add(oidSystem.Append(2, 0), berOID(oidLinuxDevice))
	add(oidSystem.Append(3, 0), berUint(berTimeTicks, uint64(uptime*100)&0xffffffff))
	add(oidSystem.Append(4, 0), berString(settings.Contact))
	add(oidSystem.Append(5, 0), berString(name))
	add(oidSystem.Append(6, 0), berString(settings.Location))
	add(oidSystem.Append(7, 0), berInt(2)) // layer 2 device

	ifaces := snmpInterfaces(st.Interfaces)
	add(oidIfNumber, berInt(int64(len(ifaces))))
	label := func(i snmpInterface) string {
		if i.Port > 0 {
			return fmt.Sprintf("ETH %d", i.Port)
		}
		return i.Name
	}
	status := func(up bool) []byte {
		if up {
			return berInt(1)
		}
		return berInt(2)
	}
	ifColumns := []struct {
		col   uint32
		value func(snmpInterface) []byte
	}{
		{1, func(i snmpInterface) []byte { return berInt(int64(i.index)) }},
		{2, func(i snmpInterface) []byte { return berString(fmt.Sprintf("%s (%s)", label(i), i.Name)) }},
		{3, func(snmpInterface) []byte { return berInt(6) }}, // ethernetCsmacd
		{5, func(i snmpInterface) []byte { return gauge(i.mbps() * 1_000_000) }},
		{6, func(i snmpInterface) []byte { return berTLV(berOctetString, macBytes(i.MAC)) }},
		{7, func(snmpInterface) []byte { return berInt(1) }},
		{8, func(i snmpInterface) []byte { return status(i.Up) }},
		{10, func(i snmpInterface) []byte { return counter(i.InOctets) }},
		{13, func(i snmpInterface) []byte { return counter(i.InDiscard) }},
		{14, func(i snmpInterface) []byte { return counter(i.InErrors) }},
		{16, func(i snmpInterface) []byte { return counter(i.OutOctets) }},
		{19, func(i snmpInterface) []byte { return counter(i.OutDiscard) }},
		{20, func(i snmpInterface) []byte { return counter(i.OutErrors) }},
	}
	for _, c := range ifColumns {
		for _, i := range ifaces {
			add(oidIfEntry.Append(c.col, i.index), c.value(i))
		}
	}
	for _, i := range ifaces {
		add(oidIfXEntry.Append(1, i.index), berString(label(i)))
	}
	for _, col := range []uint32{6, 10} {
		for _, i := range ifaces {
			octets := i.InOctets
			if col == 10 {
				octets = i.OutOctets
			}
			view = append(view, snmpVar{oid: oidIfXEntry.Append(col, i.index), value: berUint(berCounter64, uint64(max(octets, 0))), wide: true})
		}
	}
	for _, i := range ifaces {
		add(oidIfXEntry.Append(15, i.index), gauge(i.mbps()))
	}
	for _, i := range ifaces {
		alias := i.Role
		if alias == "uplink" {
			alias = "Uplink"
		} else if alias == "backup" {
			alias = "Backup uplink"
		}
		add(oidIfXEntry.Append(18, i.index), berString(alias))
	}
	sort.Slice(view, func(a, b int) bool { return view[a].oid.Compare(view[b].oid) < 0 })
	return view
}

// ------------------------------------------------------------------ HTTP API

type snmpStatus struct {
	SNMPSettings
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

func (s *SNMPAgent) status(err error) snmpStatus {
	st := snmpStatus{SNMPSettings: s.cfg.SNMPSettings()}
	st.Listen = ""
	s.mu.Lock()
	st.Running = s.conn != nil
	s.mu.Unlock()
	if err != nil {
		st.Error = err.Error()
	}
	return st
}

func (a *API) snmpSettings(w http.ResponseWriter, r *http.Request) {
	st := a.snmp.status(nil)
	if !a.isAdmin(r) {
		st.Community = "" // read-only accounts do not see the shared secret
	}
	reply(w, http.StatusOK, st)
}

func (a *API) updateSNMP(w http.ResponseWriter, r *http.Request) {
	var input SNMPSettings
	if !decode(w, r, &input) {
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.cfg.SetSNMP(input); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	st := a.snmp.status(a.snmp.Apply())
	if !a.isAdmin(r) {
		st.Community = ""
	}
	reply(w, http.StatusOK, st)
}
