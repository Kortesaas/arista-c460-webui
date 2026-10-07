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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const bootstrapAPIUser = "c460webui"

var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

type bootstrapOptions struct {
	configPath string
	country    string // ISO 3166 code; "" keeps the provisioned one
	loginUser  string // factory API login, used only when config.json has none
	loginPass  string
}

type bootstrapAgent interface {
	getAs(context.Context, string, string, *gpb.Path) (map[string]any, error)
	setAs(context.Context, string, string, *gpb.Path, any) error
	apPath(...*gpb.PathElem) *gpb.Path
}

type bootstrapCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
	return bootstrapWithAgent(opts, mac, g)
}

func bootstrapWithAgent(opts bootstrapOptions, mac string, g bootstrapAgent) error {
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
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", opts.configPath, err)
	}
	if cfg == nil {
		return errors.New("configuration must be a JSON object")
	}
	gnmiCfg, _ := cfg["gnmi"].(map[string]any)
	if gnmiCfg == nil {
		gnmiCfg = map[string]any{}
	}
	apiUser, _ := gnmiCfg["username"].(string)
	apiPass, _ := gnmiCfg["password"].(string)
	saved := apiUser != "" && apiPass != ""
	pendingPath := opts.configPath + ".bootstrap"
	pending := false
	if !saved {
		if raw, err := os.ReadFile(pendingPath); err == nil {
			var creds bootstrapCredentials
			if err := json.Unmarshal(raw, &creds); err != nil || creds.Username == "" || creds.Password == "" {
				return fmt.Errorf("bootstrap recovery file %s is invalid; keep it for recovery", pendingPath)
			}
			apiUser, apiPass, pending = creds.Username, creds.Password, true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("read bootstrap recovery file: %w", err)
		}
	}
	loginUser, loginPass := opts.loginUser, opts.loginPass
	if saved || pending {
		loginUser, loginPass = apiUser, apiPass // already set up: stay with our own user
		fmt.Printf("Using saved API user %q\n", apiUser)
	}
	hostname := strings.ReplaceAll(mac, ":", "-")
	ctx := context.Background()
	provPath := &gpb.Path{Origin: g.apPath().Origin, Elem: []*gpb.PathElem{elem("provision-aps"), elem("provision-ap", "mac", mac)}}
	provisions := &gpb.Path{Origin: provPath.Origin, Elem: []*gpb.PathElem{elem("provision-aps")}}
	current, err := g.getAs(ctx, loginUser, loginPass, provisions)
	if err != nil && status.Code(err) != codes.NotFound && pending {
		// An interrupted run may have saved the replacement credentials before
		// the AP accepted them. Only that recovery state permits factory login.
		loginUser, loginPass = opts.loginUser, opts.loginPass
		current, err = g.getAs(ctx, loginUser, loginPass, provisions)
	}
	// Factory-fresh firmware returns NotFound until the first AP is provisioned.
	// Authentication and transport failures must still stop setup.
	if status.Code(err) == codes.NotFound {
		current, err = map[string]any{}, nil
	}
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
	currentAP, err := g.getAs(ctx, loginUser, loginPass, g.apPath())
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("read AP configuration before setup: %w", err)
	}
	initializeRadios := len(items(normalize(currentAP), "radios", "radio")) == 0
	if !saved && !pending {
		token, err := newToken()
		if err != nil {
			return err
		}
		apiUser, apiPass = bootstrapAPIUser, token
		raw, err := json.Marshal(bootstrapCredentials{Username: apiUser, Password: apiPass})
		if err != nil {
			return err
		}
		// Persist before provisioning or retiring the factory login. A radio
		// restart, reboot or lost response must not lose the only API password.
		if err := atomicNative(pendingPath, raw, 0o600); err != nil {
			return fmt.Errorf("save bootstrap recovery credentials: %w", err)
		}
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
		"system": map[string]any{
			"ssh-server": map[string]any{"config": map[string]any{"enable": true}},
			"aaa": map[string]any{"authentication": map[string]any{"users": map[string]any{"user": []any{
				map[string]any{"username": apiUser, "config": map[string]any{"username": apiUser, "password": apiPass}},
			}}}},
		},
	}
	if initializeRadios {
		apBody["radios"] = map[string]any{"radio": bootstrapRadios()}
	}
	var setErr error
	for attempt := 0; attempt < 6; attempt++ { // provisioning can take a moment to create the AP entry
		if setErr = g.setAs(ctx, loginUser, loginPass, g.apPath(), apBody); setErr == nil {
			break
		}
		if _, err := g.getAs(ctx, apiUser, apiPass, g.apPath()); err == nil {
			// The update reached the AP even if its response was lost.
			setErr = nil
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
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("API credentials saved, but recovery file cleanup failed: %w", err)
	}
	fmt.Printf("OpenConfig API user %q is ready and saved in %s\n", apiUser, opts.configPath)
	return nil
}

// These are the C-460's three configurable OpenConfig radios. Their ids differ
// from the native wifi interface numbers; the fourth radio is for scanning.
// Initialize only an empty configuration so later runs preserve user settings.
func bootstrapRadios() []any {
	radios := []any{}
	for _, r := range []struct {
		id                    int
		frequency             string
		channel, width, power int
	}{
		{0, "FREQ_2GHZ", 1, 20, 20},
		{1, "FREQ_5GHZ", 36, 80, 23},
		{2, "FREQ_6GHZ", 5, 80, 23},
	} {
		radios = append(radios, map[string]any{
			"id": r.id, "operating-frequency": r.frequency,
			"config": map[string]any{
				"id": r.id, "operating-frequency": r.frequency, "enabled": true,
				"channel": r.channel, "channel-width": r.width, "transmit-power": r.power,
				"dca": false, "dtp": false, "scanning": false,
			},
		})
	}
	return radios
}
