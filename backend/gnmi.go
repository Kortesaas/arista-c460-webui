package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// GNMI talks to the AP's local OpenConfig agent. The agent's certificate has
// no SAN, so instead of hostname verification the exact certificate on disk is pinned.
type GNMI struct {
	cfg    GNMIConfig
	host   string
	conn   *grpc.ClientConn
	client gpb.GNMIClient
}

func DialGNMI(cfg GNMIConfig, hostname string) (*GNMI, error) {
	pemBytes, err := os.ReadFile(cfg.CertFile)
	if err != nil {
		return nil, fmt.Errorf("read agent certificate: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("agent certificate is not PEM")
	}
	pin := sha256.Sum256(block.Bytes)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // replaced by the exact pin below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || sha256.Sum256(raw[0]) != pin {
				return errors.New("gNMI agent certificate does not match the pinned certificate")
			}
			return nil
		},
	}
	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithAuthority(cfg.ServerName))
	if err != nil {
		return nil, err
	}
	return &GNMI{cfg: cfg, host: hostname, conn: conn, client: gpb.NewGNMIClient(conn)}, nil
}

func (g *GNMI) Close() { _ = g.conn.Close() }

func (g *GNMI) ctx(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	return metadata.AppendToOutgoingContext(ctx, "username", g.cfg.Username, "password", g.cfg.Password), cancel
}

func elem(name string, keys ...string) *gpb.PathElem {
	e := &gpb.PathElem{Name: name}
	for i := 0; i+1 < len(keys); i += 2 {
		if e.Key == nil {
			e.Key = map[string]string{}
		}
		e.Key[keys[i]] = keys[i+1]
	}
	return e
}

// apPath returns /access-points/access-point[hostname=H]/<rest...>.
func (g *GNMI) apPath(rest ...*gpb.PathElem) *gpb.Path {
	elems := append([]*gpb.PathElem{elem("access-points"), elem("access-point", "hostname", g.host)}, rest...)
	return &gpb.Path{Origin: g.cfg.Origin, Elem: elems}
}

// GetAP returns the whole access-point subtree as JSON.
func (g *GNMI) GetAP(parent context.Context) (json.RawMessage, error) {
	ctx, cancel := g.ctx(parent, 20*time.Second)
	defer cancel()
	resp, err := g.client.Get(ctx, &gpb.GetRequest{Path: []*gpb.Path{g.apPath()}, Encoding: gpb.Encoding_JSON_IETF})
	if err != nil {
		return nil, err
	}
	for _, n := range resp.GetNotification() {
		for _, u := range n.GetUpdate() {
			if v := u.GetVal().GetJsonIetfVal(); v != nil {
				return v, nil
			}
			if v := u.GetVal().GetJsonVal(); v != nil {
				return v, nil
			}
		}
	}
	return nil, errors.New("empty gNMI response")
}

// SetAP merges body into the access-point and applies deletes in the same
// transaction. The API user is always re-sent: this firmware resets API
// authentication when an access-point update omits it.
func (g *GNMI) SetAP(parent context.Context, body map[string]any, deletes []*gpb.Path) error {
	body["hostname"] = g.host
	body["config"] = map[string]any{"hostname": g.host}
	system, _ := body["system"].(map[string]any)
	if system == nil {
		system = map[string]any{}
	}
	system["aaa"] = map[string]any{"authentication": map[string]any{"users": map[string]any{"user": []any{
		map[string]any{"username": g.cfg.Username, "config": map[string]any{"username": g.cfg.Username, "password": g.cfg.Password}},
	}}}}
	body["system"] = system
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return err
	}
	ctx, cancel := g.ctx(parent, 30*time.Second)
	defer cancel()
	_, err := g.client.Set(ctx, &gpb.SetRequest{
		Delete: deletes,
		Update: []*gpb.Update{{Path: g.apPath(), Val: &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: bytes.TrimSpace(buf.Bytes())}}}},
	})
	return err
}
