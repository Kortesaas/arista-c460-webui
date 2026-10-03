package main

// First-time setup of a freshly unlocked AP: creates the OpenConfig API user
// this service signs in with, provisions hostname and regulatory country, and
// writes config.json. Run on the AP (deploy.sh --bootstrap does this). It is
// idempotent: with existing credentials in config.json it reuses them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/metadata"
)

const bootstrapAPIUser = "c460webui"

var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type bootstrapOptions struct {
	configPath string
	country    string // ISO 3166 code; "" keeps the provisioned one
	loginUser  string // factory API login, used only when config.json has none
	loginPass  string
}

// set sends one JSON_IETF update authenticated as user/pass.
func (g *GNMI) setAs(parent context.Context, user, pass string, path *gpb.Path, value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "username", user, "password", pass)
	_, err := g.client.Set(ctx, &gpb.SetRequest{Update: []*gpb.Update{{Path: path, Val: &gpb.TypedValue{Value: &gpb.TypedValue_JsonIetfVal{JsonIetfVal: bytes.TrimSpace(buf.Bytes())}}}}})
	return err
}

func (g *GNMI) getAs(parent context.Context, user, pass string, path *gpb.Path) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "username", user, "password", pass)
	resp, err := g.client.Get(ctx, &gpb.GetRequest{Path: []*gpb.Path{path}, Encoding: gpb.Encoding_JSON_IETF})
	if err != nil {
		return nil, err
	}
	for _, n := range resp.GetNotification() {
		for _, u := range n.GetUpdate() {
			raw := u.GetVal().GetJsonIetfVal()
			if raw == nil {
				raw = u.GetVal().GetJsonVal()
			}
			var out map[string]any
			if err := json.Unmarshal(raw, &out); err == nil {
				return out, nil
			}
		}
	}
	return map[string]any{}, nil
}

func runBootstrap(opts bootstrapOptions) error {
	opts.country = strings.ToUpper(strings.TrimSpace(opts.country))
	if opts.country != "" && !countryPattern.MatchString(opts.country) {
		return fmt.Errorf("country must be a two-letter code such as DE, got %q", opts.country)
	}

	// Existing configuration, kept as a generic map so unknown keys survive.
	cfg := map[string]any{}
	if raw, err := os.ReadFile(opts.configPath); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("%s: %w", opts.configPath, err)
		}
	}
	gnmiCfg, _ := cfg["gnmi"].(map[string]any)
	if gnmiCfg == nil {
		gnmiCfg = map[string]any{}
	}
	apiUser, _ := gnmiCfg["username"].(string)
	apiPass, _ := gnmiCfg["password"].(string)
	loginUser, loginPass := opts.loginUser, opts.loginPass
	if apiUser != "" && apiPass != "" {
		loginUser, loginPass = apiUser, apiPass // already set up: stay with our own user
		fmt.Printf("Using the existing API user %q from %s\n", apiUser, opts.configPath)
	} else {
		token, err := newToken()
		if err != nil {
			return err
		}
		apiUser, apiPass = bootstrapAPIUser, token
	}

	macRaw, err := os.ReadFile("/sys/class/net/eth0/address")
	if err != nil {
		return fmt.Errorf("read eth0 MAC: %w", err)
	}
	mac := strings.ToUpper(strings.TrimSpace(string(macRaw)))
	hostname := strings.ReplaceAll(mac, ":", "-")

	defaults := GNMIConfig{Address: "127.0.0.1:8080", CertFile: "/opt/openconfig/cert/agent.crt", ServerName: "openconfig.mojonetworks.com", Origin: "openconfig.mojonetworks.com"}
	g, err := DialGNMI(defaults, hostname)
	if err != nil {
		return fmt.Errorf("OpenConfig agent: %w (is OpenConfig mode enabled?)", err)
	}
	defer g.Close()
	ctx := context.Background()

	provPath := &gpb.Path{Origin: defaults.Origin, Elem: []*gpb.PathElem{elem("provision-aps"), elem("provision-ap", "mac", mac)}}
	current, err := g.getAs(ctx, loginUser, loginPass, &gpb.Path{Origin: defaults.Origin, Elem: []*gpb.PathElem{elem("provision-aps")}})
	if err != nil {
		return fmt.Errorf("sign-in to the OpenConfig agent as %q failed: %w", loginUser, err)
	}
	existingCountry := ""
	// The agent answers either with the container or with its contents.
	tree := normalize(current)
	list, _ := dig(tree, "provision-ap").([]any)
	if list == nil {
		list = items(tree, "provision-aps", "provision-ap")
	}
	for _, p := range list {
		if strings.EqualFold(str(dig(p, "mac")), mac) {
			existingCountry = str(dig(p, "config", "country-code"))
		}
	}
	country := opts.country
	if country == "" {
		country = existingCountry
	}
	if country == "" {
		return errors.New("this AP is not provisioned yet: pass the regulatory country, e.g. --country DE")
	}
	if country != existingCountry {
		fmt.Printf("Provisioning %s as %s, country %s. A country change makes the AP restart its radios or reboot.\n", mac, hostname, country)
		if err := g.setAs(ctx, loginUser, loginPass, provPath, map[string]any{"mac": mac, "config": map[string]any{"hostname": hostname, "country-code": country}}); err != nil {
			return fmt.Errorf("provisioning failed: %w", err)
		}
	} else {
		fmt.Printf("Already provisioned as %s, country %s\n", hostname, country)
	}

	// The firmware replaces the API user list on every access-point update,
	// so this one update both creates our user and retires the factory login.
	apBody := map[string]any{
		"hostname": hostname,
		"config":   map[string]any{"hostname": hostname},
		"system": map[string]any{"aaa": map[string]any{"authentication": map[string]any{"users": map[string]any{"user": []any{
			map[string]any{"username": apiUser, "config": map[string]any{"username": apiUser, "password": apiPass}},
		}}}}},
	}
	var setErr error
	for attempt := 0; attempt < 6; attempt++ { // provisioning can take a moment to create the AP entry
		if setErr = g.setAs(ctx, loginUser, loginPass, g.apPath(), apBody); setErr == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if setErr != nil {
		return fmt.Errorf("creating the API user failed: %w", setErr)
	}
	if _, err := g.getAs(ctx, apiUser, apiPass, g.apPath()); err != nil {
		return fmt.Errorf("the new API user does not work: %w", err)
	}

	gnmiCfg["username"], gnmiCfg["password"] = apiUser, apiPass
	cfg["gnmi"] = gnmiCfg
	for k, v := range map[string]any{"listen": ":80", "pollSeconds": 5, "authFile": "/opt/c460-webui/auth.json"} {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := atomicNative(opts.configPath, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("OpenConfig API user %q is ready and saved in %s\n", apiUser, opts.configPath)
	return nil
}
