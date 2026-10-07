package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const bootstrapTestMAC = "02:00:00:46:00:01"

type testBootstrapAgent struct {
	t            *testing.T
	configPath   string
	country      string
	user, pass   string
	interrupt    bool
	lostResponse bool
	unready      bool
	factoryReads int
	writes       int
	missingTree  bool
	readError    error
	radios       []any
	radioWrites  int
}

func (g *testBootstrapAgent) apPath(rest ...*gpb.PathElem) *gpb.Path {
	return &gpb.Path{Origin: "openconfig.mojonetworks.com", Elem: append([]*gpb.PathElem{elem("access-points"), elem("access-point", "hostname", "02-00-00-46-00-01")}, rest...)}
}

func (g *testBootstrapAgent) getAs(_ context.Context, user, pass string, path *gpb.Path) (map[string]any, error) {
	if user == "admin" {
		g.factoryReads++
	}
	valid := (g.user == "" && user == "admin" && pass == "admin") || (g.user != "" && user == g.user && pass == g.pass)
	if !valid {
		return nil, errors.New("unauthenticated")
	}
	if path.Elem[0].Name == "provision-aps" {
		if g.readError != nil {
			return nil, g.readError
		}
		if g.missingTree && g.country == "" {
			return nil, status.Error(codes.NotFound, "provision-aps not found")
		}
		return map[string]any{"provision-ap": []any{map[string]any{"mac": bootstrapTestMAC, "config": map[string]any{"country-code": g.country}}}}, nil
	}
	if g.unready && g.user != "" {
		g.unready = false
		return nil, errors.New("agent restarting")
	}
	return map[string]any{"hostname": "02-00-00-46-00-01", "radios": map[string]any{"radio": g.radios}}, nil
}

func (g *testBootstrapAgent) setAs(_ context.Context, _, _ string, path *gpb.Path, value any) error {
	g.writes++
	if g.user == "" {
		info, err := os.Stat(g.configPath + ".bootstrap")
		if err != nil || info.Mode().Perm() != 0o600 {
			g.t.Fatal("private recovery credentials must exist before the first AP change")
		}
	}
	body := value.(map[string]any)
	if path.Elem[0].Name == "provision-aps" {
		g.country = body["config"].(map[string]any)["country-code"].(string)
		if g.interrupt {
			g.interrupt = false
			return errors.New("connection lost during country provisioning")
		}
		return nil
	}
	if dig(body, "system", "ssh-server", "config", "enable") != true {
		g.t.Fatal("bootstrap must enable the native SSH service for deployment and future updates")
	}
	if radios, ok := dig(body, "radios", "radio").([]any); ok {
		g.radios = radios
		g.radioWrites++
	}
	users := dig(body, "system", "aaa", "authentication", "users", "user").([]any)
	config := users[0].(map[string]any)["config"].(map[string]any)
	g.user, g.pass = config["username"].(string), config["password"].(string)
	if g.lostResponse {
		g.lostResponse = false
		return errors.New("response lost after API login changed")
	}
	return nil
}

func TestBootstrapFreshAndInterruptedSetup(t *testing.T) {
	for _, scenario := range []string{"fresh", "factory-missing-tree", "country-restart", "lost-response", "verification-restart"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(`{"siteName":"Keep this","pollSeconds":10,"custom":{"keep":true}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			g := &testBootstrapAgent{t: t, configPath: path, missingTree: scenario == "factory-missing-tree", interrupt: scenario == "country-restart", lostResponse: scenario == "lost-response", unready: scenario == "verification-restart"}
			opts := bootstrapOptions{configPath: path, country: "de", loginUser: "admin", loginPass: "admin"}
			err := bootstrapWithAgent(opts, bootstrapTestMAC, g)
			if scenario == "country-restart" || scenario == "verification-restart" {
				if err == nil {
					t.Fatal("interrupted setup should report failure and retain recovery credentials")
				}
				raw, readErr := os.ReadFile(path + ".bootstrap")
				var recovery bootstrapCredentials
				if readErr != nil || json.Unmarshal(raw, &recovery) != nil || recovery.Password == "" {
					t.Fatal("interrupted setup lost recovery credentials")
				}
				err = bootstrapWithAgent(opts, bootstrapTestMAC, g)
				if g.pass != recovery.Password {
					t.Fatal("retry must reuse the same saved API password")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(g.radios) != 3 || g.radioWrites != 1 {
				t.Fatal("fresh setup must initialize the three configurable radios once")
			}
			raw, _ := os.ReadFile(path)
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil || cfg["siteName"] != "Keep this" || cfg["pollSeconds"] != float64(10) || dig(cfg, "custom", "keep") != true || dig(cfg, "gnmi", "password") != g.pass {
				t.Fatal("setup must preserve existing settings and save working credentials")
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Fatal("config must remain private")
			}
			if _, err := os.Stat(path + ".bootstrap"); !os.IsNotExist(err) {
				t.Fatal("successful setup must remove recovery file")
			}
			factoryReads, password := g.factoryReads, g.pass
			if err := bootstrapWithAgent(opts, bootstrapTestMAC, g); err != nil || g.factoryReads != factoryReads || g.pass != password || g.radioWrites != 1 {
				t.Fatal("a configured AP must reuse its API login without the factory login", err)
			}
		})
	}
}

func TestBootstrapPreservesConfiguredRadios(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	g := &testBootstrapAgent{t: t, configPath: path, country: "DE", radios: []any{
		map[string]any{"id": 1, "operating-frequency": "FREQ_5GHZ", "config": map[string]any{"enabled": false, "channel": 100, "transmit-power": 15}},
	}}
	opts := bootstrapOptions{configPath: path, country: "DE", loginUser: "admin", loginPass: "admin"}
	if err := bootstrapWithAgent(opts, bootstrapTestMAC, g); err != nil || g.radioWrites != 0 || len(g.radios) != 1 {
		t.Fatal("bootstrap must preserve existing radio configuration", err)
	}
}

func TestBootstrapStopsOnAgentReadFailure(t *testing.T) {
	for _, code := range []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			g := &testBootstrapAgent{t: t, configPath: path, readError: status.Error(code, "agent read failed")}
			opts := bootstrapOptions{configPath: path, country: "DE", loginUser: "admin", loginPass: "admin"}
			if err := bootstrapWithAgent(opts, bootstrapTestMAC, g); err == nil || g.writes != 0 {
				t.Fatal("API read failure must stop bootstrap before changing the AP")
			}
			if _, err := os.Stat(path + ".bootstrap"); !os.IsNotExist(err) {
				t.Fatal("failed sign-in must not create recovery credentials")
			}
		})
	}
}

func TestBootstrapRejectsInvalidSetupBeforeAPChanges(t *testing.T) {
	for _, scenario := range []string{"missing-country", "invalid-country", "null-config", "invalid-recovery", "wrong-saved-login"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			opts := bootstrapOptions{configPath: path, loginUser: "admin", loginPass: "admin"}
			switch scenario {
			case "invalid-country":
				opts.country = "Germany"
			case "null-config":
				_ = os.WriteFile(path, []byte(`null`), 0o600)
			case "invalid-recovery":
				_ = os.WriteFile(path+".bootstrap", []byte(`{"username":"c460webui"}`), 0o600)
			case "wrong-saved-login":
				_ = os.WriteFile(path, []byte(`{"gnmi":{"username":"existing","password":"wrong"}}`), 0o600)
			}
			g := &testBootstrapAgent{t: t, configPath: path}
			if err := bootstrapWithAgent(opts, bootstrapTestMAC, g); err == nil || g.writes != 0 {
				t.Fatal("invalid setup must fail before modifying the AP")
			}
			if scenario == "wrong-saved-login" && g.factoryReads != 0 {
				t.Fatal("existing credentials must not silently fall back to the factory login")
			}
		})
	}
}
