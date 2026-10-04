package main

// Minimal BER (ASN.1) encoding and decoding, just enough for the read-only
// SNMP agent in snmp.go.

import (
	"errors"
	"strconv"
	"strings"
)

const (
	berInteger        = 0x02
	berOctetString    = 0x04
	berNull           = 0x05
	berObjectID       = 0x06
	berSequence       = 0x30
	berCounter32      = 0x41
	berGauge32        = 0x42
	berTimeTicks      = 0x43
	berCounter64      = 0x46
	berNoSuchObject   = 0x80
	berNoSuchInstance = 0x81
	berEndOfMibView   = 0x82

	pduGet      = 0xa0
	pduGetNext  = 0xa1
	pduResponse = 0xa2
	pduSet      = 0xa3
	pduGetBulk  = 0xa5
)

var errBER = errors.New("malformed BER")

// OID is an object identifier such as 1.3.6.1.2.1.1.1.0.
type OID []uint32

func mustOID(s string) OID {
	var o OID
	for _, part := range strings.Split(s, ".") {
		v, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			panic("bad OID " + s)
		}
		o = append(o, uint32(v))
	}
	return o
}

// Append returns a new OID; the receiver is never modified.
func (o OID) Append(arcs ...uint32) OID {
	out := make(OID, 0, len(o)+len(arcs))
	return append(append(out, o...), arcs...)
}

func (o OID) Compare(p OID) int {
	for i := 0; i < len(o) && i < len(p); i++ {
		if o[i] != p[i] {
			if o[i] < p[i] {
				return -1
			}
			return 1
		}
	}
	return len(o) - len(p)
}

func (o OID) String() string {
	parts := make([]string, len(o))
	for i, v := range o {
		parts[i] = strconv.FormatUint(uint64(v), 10)
	}
	return strings.Join(parts, ".")
}

// ------------------------------------------------------------------ encoding

func berTLV(tag byte, content []byte) []byte {
	out := make([]byte, 0, len(content)+4)
	out = append(out, tag)
	switch n := len(content); {
	case n < 0x80:
		out = append(out, byte(n))
	case n <= 0xff:
		out = append(out, 0x81, byte(n))
	case n <= 0xffff:
		out = append(out, 0x82, byte(n>>8), byte(n))
	default:
		out = append(out, 0x83, byte(n>>16), byte(n>>8), byte(n))
	}
	return append(out, content...)
}

func berSeq(tag byte, parts ...[]byte) []byte {
	var content []byte
	for _, p := range parts {
		content = append(content, p...)
	}
	return berTLV(tag, content)
}

// berInt encodes a signed integer in the shortest two's complement form.
func berInt(v int64) []byte {
	n := 1
	for x := v; x > 127 || x < -128; x >>= 8 {
		n++
	}
	b := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return berTLV(berInteger, b)
}

// berUint encodes the unsigned application types (Counter32, Gauge32, TimeTicks, Counter64).
func berUint(tag byte, v uint64) []byte {
	n := 1
	for x := v; x > 0x7f; x >>= 8 {
		n++
	}
	b := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return berTLV(tag, b)
}

func berString(s string) []byte { return berTLV(berOctetString, []byte(s)) }

func berOID(o OID) []byte {
	if len(o) < 2 {
		return berTLV(berObjectID, []byte{0})
	}
	b := appendBase128(nil, o[0]*40+o[1])
	for _, arc := range o[2:] {
		b = appendBase128(b, arc)
	}
	return berTLV(berObjectID, b)
}

func appendBase128(b []byte, v uint32) []byte {
	var tmp [5]byte
	i := len(tmp) - 1
	tmp[i] = byte(v & 0x7f)
	for v >>= 7; v > 0; v >>= 7 {
		i--
		tmp[i] = byte(v&0x7f) | 0x80
	}
	return append(b, tmp[i:]...)
}

// ------------------------------------------------------------------ decoding

type berReader struct{ b []byte }

// next returns the tag, the content and the complete encoded element.
func (r *berReader) next() (tag byte, content, raw []byte, err error) {
	if len(r.b) < 2 {
		return 0, nil, nil, errBER
	}
	tag, length, header := r.b[0], int(r.b[1]), 2
	if length&0x80 != 0 {
		k := length & 0x7f
		if k == 0 || k > 2 || len(r.b) < 2+k {
			return 0, nil, nil, errBER
		}
		length = 0
		for _, x := range r.b[2 : 2+k] {
			length = length<<8 | int(x)
		}
		header += k
	}
	if len(r.b)-header < length {
		return 0, nil, nil, errBER
	}
	raw = r.b[:header+length]
	content = r.b[header : header+length]
	r.b = r.b[header+length:]
	return tag, content, raw, nil
}

func (r *berReader) expect(tag byte) ([]byte, error) {
	got, content, _, err := r.next()
	if err != nil {
		return nil, err
	}
	if got != tag {
		return nil, errBER
	}
	return content, nil
}

func (r *berReader) integer() (int64, error) {
	c, err := r.expect(berInteger)
	if err != nil {
		return 0, err
	}
	if len(c) == 0 || len(c) > 8 {
		return 0, errBER
	}
	v := int64(int8(c[0]))
	for _, x := range c[1:] {
		v = v<<8 | int64(x)
	}
	return v, nil
}

func parseOID(c []byte) (OID, error) {
	if len(c) == 0 {
		return nil, errBER
	}
	var o OID
	var v uint32
	for i, x := range c {
		if v >= 1<<25 {
			return nil, errBER
		}
		v = v<<7 | uint32(x&0x7f)
		if x&0x80 != 0 {
			if i == len(c)-1 {
				return nil, errBER
			}
			continue
		}
		if o == nil {
			switch {
			case v < 40:
				o = OID{0, v}
			case v < 80:
				o = OID{1, v - 40}
			default:
				o = OID{2, v - 80}
			}
		} else {
			o = append(o, v)
		}
		if len(o) > 128 {
			return nil, errBER
		}
		v = 0
	}
	return o, nil
}
