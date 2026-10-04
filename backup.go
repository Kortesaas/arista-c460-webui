package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"golang.org/x/crypto/scrypt"
)

// A backup is a portable JSON description of the AP's configuration. It can
// be restored on the same AP or copied to another C-460. Wi-Fi passwords are
// only included when a passphrase is given, and then only encrypted.

const backupFormat = "c460-webui-backup"

type Backup struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Source    struct {
		Model    string `json:"model"`
		Hostname string `json:"hostname"`
		Firmware string `json:"firmware"`
		UI       string `json:"ui"`
	} `json:"source"`
	SSIDs  []ssidRequest  `json:"ssids"`
	Radios []BackupRadio  `json:"radios"`
	WiFi7  *WiFi7Settings `json:"wifi7,omitempty"`
	// Advanced settings that are not at the firmware default, per SSID name
	// and per radio band (see features.go).
	SSIDFeatures  map[string]map[string]any `json:"ssidFeatures,omitempty"`
	RadioFeatures map[string]map[string]any `json:"radioFeatures,omitempty"`
	Management    *ManagementRequest        `json:"management,omitempty"`
	Labels        BackupLabels              `json:"labels"`
	Time          *TimeInput                `json:"time,omitempty"`
	LLDP          *LLDPTiming               `json:"lldp,omitempty"`
	Secrets       *EncryptedSecrets         `json:"secrets,omitempty"`
}

type BackupRadio struct {
	Band string `json:"band"`
	radioRequest
}

type BackupLabels struct {
	SiteName  string            `json:"siteName"`
	VLANNames map[string]string `json:"vlanNames"`
}

// EncryptedSecrets holds {"ssid name": "password"} encrypted with AES-256-GCM
// under a key derived from the passphrase with scrypt.
type EncryptedSecrets struct {
	KDF        string `json:"kdf"`
	N          int    `json:"n"`
	R          int    `json:"r"`
	P          int    `json:"p"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

const scryptN, scryptR, scryptP = 1 << 15, 8, 1

func secretsKey(passphrase string, salt []byte, n, r, p int) ([]byte, error) {
	if n < 1<<14 || n > 1<<20 || r < 1 || r > 16 || p < 1 || p > 4 {
		return nil, errors.New("unsupported key derivation parameters")
	}
	return scrypt.Key([]byte(passphrase), salt, n, r, p, 32)
}

func encryptSecrets(passphrase string, secrets map[string]string) (*EncryptedSecrets, error) {
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := secretsKey(passphrase, salt, scryptN, scryptR, scryptP)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, _ := json.Marshal(secrets)
	return &EncryptedSecrets{KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP, Salt: salt, Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, plain, []byte(backupFormat))}, nil
}

var errWrongPassphrase = errors.New("wrong passphrase, or the backup's password section is damaged")

func (e *EncryptedSecrets) decrypt(passphrase string) (map[string]string, error) {
	if e.KDF != "scrypt" || len(e.Nonce) != 12 {
		return nil, errors.New("unsupported password encryption in backup")
	}
	key, err := secretsKey(passphrase, e.Salt, e.N, e.R, e.P)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, e.Nonce, e.Ciphertext, []byte(backupFormat))
	if err != nil {
		return nil, errWrongPassphrase
	}
	var secrets map[string]string
	if err := json.Unmarshal(plain, &secrets); err != nil {
		return nil, errWrongPassphrase
	}
	return secrets, nil
}

// ------------------------------------------------------------------ export

func (a *API) createBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Passphrase string `json:"passphrase"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Passphrase != "" && len(body.Passphrase) < 8 {
		fail(w, http.StatusBadRequest, "the passphrase must have at least 8 characters")
		return
	}
	st := a.snapshot()
	if st.GeneratedAt.IsZero() {
		fail(w, http.StatusServiceUnavailable, "The AP configuration has not been read yet. Try again in a few seconds.")
		return
	}
	b := Backup{Format: backupFormat, Version: 1, CreatedAt: time.Now().UTC()}
	b.Source.Model, b.Source.Hostname, b.Source.Firmware, b.Source.UI = st.Device.Model, st.Device.Hostname, st.Device.Firmware, version
	secrets := map[string]string{}
	for _, s := range st.SSIDs {
		b.SSIDs = append(b.SSIDs, ssidRequest{Name: s.Name, Enabled: s.Enabled, Hidden: s.Hidden, OpMode: s.OpMode, Bands: s.Bands, VLAN: s.VLAN, Isolation: s.Isolation})
		if entry, ok := a.poller.SSIDEntry(s.Name); ok {
			if f := setFeatures(featureValues(entry, ssidFeatureDefs)); len(f) > 0 {
				if b.SSIDFeatures == nil {
					b.SSIDFeatures = map[string]map[string]any{}
				}
				b.SSIDFeatures[s.Name] = f
			}
		}
		if cfg, ok := a.poller.SSIDConfig(s.Name); ok {
			if psk := str(firstOf(cfg["wpa3-psk"], cfg["wpa2-psk"])); psk != "" {
				secrets[s.Name] = psk
			}
		}
	}
	for _, radio := range st.Radios {
		b.Radios = append(b.Radios, BackupRadio{Band: radio.Band, radioRequest: radioRequest{Enabled: radio.Enabled, Channel: radio.Channel, Width: radio.Width, Power: radio.PowerRequested, DCA: radio.DCA, DTP: radio.DTP}})
		if cfg, _, ok := a.poller.RadioConfig(radio.ID); ok {
			if f := setFeatures(featureValues(map[string]any{"config": cfg}, radioFeatureDefs)); len(f) > 0 {
				if b.RadioFeatures == nil {
					b.RadioFeatures = map[string]map[string]any{}
				}
				b.RadioFeatures[radio.Band] = f
			}
		}
	}
	if a.wifi7 != nil {
		b.WiFi7 = a.wifi7.Saved()
	}
	if m, _, err := a.cli.Snapshot(); err == "" && m.Mode != "" {
		b.Management = &ManagementRequest{CommVLAN: m.CommVLAN, Mode: m.Mode, IPv4: m.IPv4, Netmask: m.Netmask, Gateway: m.Gateway, DNS: m.DNS, DNSSearch: m.DNSSearch}
	}
	b.Labels.SiteName, b.Labels.VLANNames = a.cfg.Labels()
	if t, err := readTimeSettings(); err == nil {
		b.Time = &TimeInput{Primary: t.Primary, Secondary: t.Secondary}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if timing, err := (lldpBackend{path: lldpSettingsFile, run: runVendorTool}).read(ctx); err == nil {
		b.LLDP = &timing
	}
	if body.Passphrase != "" {
		enc, err := encryptSecrets(body.Passphrase, secrets)
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not encrypt passwords: "+err.Error())
			return
		}
		b.Secrets = enc
	}
	log.Printf("configuration backup created by %s (passwords %s)", clientIP(r), map[bool]string{true: "included, encrypted", false: "omitted"}[b.Secrets != nil])
	reply(w, http.StatusOK, b)
}

// ----------------------------------------------------------------- restore

type restoreRequest struct {
	Backup     Backup `json:"backup"`
	Passphrase string `json:"passphrase"`
	Sections   struct {
		Wireless   bool `json:"wireless"`
		Radios     bool `json:"radios"`
		Management bool `json:"management"`
		Labels     bool `json:"labels"`
		Time       bool `json:"time"`
		LLDP       bool `json:"lldp"`
	} `json:"sections"`
	// RemoveOthers deletes SSIDs on this AP that are not in the backup.
	RemoveOthers bool `json:"removeOthers"`
	// Management overrides the backup's management settings, so a copy to
	// another AP can get its own address.
	Management *ManagementRequest `json:"management,omitempty"`
}

type restorePlan struct {
	body       map[string]any
	deletes    []*gpb.Path
	management *ManagementRequest
	comm       string
	steps      []string
	ssids      []ssidChange // mixed-mode bookkeeping after the transaction
}

// planRestore validates the whole request before anything is changed.
func (a *API) planRestore(req restoreRequest) (*restorePlan, error) {
	b := req.Backup
	if b.Format != backupFormat || b.Version != 1 {
		return nil, errors.New("this is not a C-460 web UI backup file (or it is from a newer version)")
	}
	plan := &restorePlan{body: map[string]any{}}
	var secrets map[string]string
	if req.Sections.Wireless && b.Secrets != nil && req.Passphrase != "" {
		var err error
		if secrets, err = b.Secrets.decrypt(req.Passphrase); err != nil {
			return nil, err
		}
	}
	if req.Sections.Wireless {
		var entries []any
		names := map[string]bool{}
		for _, s := range b.SSIDs {
			if names[s.Name] {
				return nil, fmt.Errorf("SSID %q appears twice in the backup", s.Name)
			}
			names[s.Name] = true
			current, exists := a.poller.SSIDConfig(s.Name)
			s.Password = secrets[s.Name]
			needsPassword := s.OpMode == "WPA3_SAE" || s.OpMode == "WPA2_PERSONAL"
			if needsPassword && s.Password == "" && str(firstOf(current["wpa3-psk"], current["wpa2-psk"])) == "" {
				return nil, fmt.Errorf("no password for %q: the backup has no passwords (or no passphrase was given) and the SSID does not exist on this AP yet", s.Name)
			}
			cfg, leaves, err := ssidConfig(s, current)
			if err != nil {
				return nil, fmt.Errorf("SSID %q: %w", s.Name, err)
			}
			entry := map[string]any{"name": s.Name, "config": cfg}
			if err := mergeFeatures(ssidFeatureDefs, entry, b.SSIDFeatures[s.Name], s.Name); err != nil {
				return nil, fmt.Errorf("advanced settings of %q: %w", s.Name, err)
			}
			entries = append(entries, entry)
			plan.ssids = append(plan.ssids, ssidChange{oldName: s.Name, newName: s.Name, mixed: s.OpMode == opModeMixed})
			if exists {
				for _, leaf := range leaves {
					plan.deletes = append(plan.deletes, a.ssidLeaf(s.Name, leaf))
				}
			}
		}
		if req.RemoveOthers {
			for _, s := range a.poller.Snapshot().SSIDs {
				if !names[s.Name] {
					plan.deletes = append(plan.deletes, a.gnmi.apPath(elem("ssids"), elem("ssid", "name", s.Name)))
					plan.ssids = append(plan.ssids, ssidChange{oldName: s.Name})
				}
			}
		}
		if len(entries) > 0 {
			plan.body["ssids"] = map[string]any{"ssid": entries}
		}
		plan.steps = append(plan.steps, fmt.Sprintf("%d wireless networks", len(entries)))
	}
	if req.Sections.Radios {
		if b.WiFi7 != nil {
			if err := b.WiFi7.validate(); err != nil {
				return nil, err
			}
			if a.wifi7 == nil || !a.wifi7.Snapshot().Supported {
				return nil, errors.New("This AP cannot restore native Wi-Fi 7 settings")
			}
			has6GHz := false
			for _, br := range b.Radios {
				if br.Band == "6" && br.Enabled {
					has6GHz = true
				}
			}
			if !has6GHz {
				return nil, errors.New("Include an enabled 6 GHz radio when restoring its Wi-Fi 7 settings")
			}
		}
		var entries []any
		for _, br := range b.Radios {
			if br.Band == "6" && !br.Enabled && b.WiFi7 != nil {
				return nil, errors.New("Enable the 6 GHz radio when restoring its Wi-Fi 7 settings")
			}
			id := -1
			for _, radio := range a.poller.Snapshot().Radios {
				if radio.Band == br.Band {
					id = radio.ID
				}
			}
			if id < 0 {
				return nil, fmt.Errorf("this AP has no %s GHz radio", br.Band)
			}
			entry, err := a.radioEntry(id, br.radioRequest)
			if err == nil {
				err = mergeFeatures(radioFeatureDefs, entry, b.RadioFeatures[br.Band], "")
			}
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
		if len(entries) > 0 {
			plan.body["radios"] = map[string]any{"radio": entries}
		}
		plan.steps = append(plan.steps, fmt.Sprintf("%d radios", len(entries)))
	}
	if req.Sections.Management {
		m := b.Management
		if req.Management != nil {
			m = req.Management
		}
		if m == nil {
			return nil, errors.New("the backup has no management network settings")
		}
		current, _, readErr := a.cli.Snapshot()
		comm := current.CommVLAN
		if m.CommVLAN != "" {
			comm = m.CommVLAN
		}
		if readErr != "" || comm == "" {
			return nil, errors.New("management settings of this AP are not ready; try again in a few seconds")
		}
		if _, err := m.cliCommand(comm); err != nil {
			return nil, fmt.Errorf("management network: %w", err)
		}
		plan.management, plan.comm = m, comm
	}
	if req.Sections.Labels {
		if utf8Len(b.Labels.SiteName) > 48 {
			return nil, errors.New("device name in backup is longer than 48 characters")
		}
	}
	if req.Sections.Time && b.Time != nil {
		if err := b.Time.validate(); err != nil {
			return nil, fmt.Errorf("time servers: %w", err)
		}
	}
	if req.Sections.LLDP && b.LLDP != nil {
		if err := b.LLDP.validate(); err != nil {
			return nil, fmt.Errorf("LLDP timing: %w", err)
		}
	}
	return plan, nil
}

func utf8Len(s string) int { return len([]rune(s)) }

func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(4 * time.Minute))
	var req restoreRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid backup: "+err.Error())
		return
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	plan, err := a.planRestore(req)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errWrongPassphrase) {
			code = http.StatusForbidden
		}
		fail(w, code, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
	defer cancel()
	var applied []string
	// Wi-Fi and radios in one transaction, so the AP restarts Wi-Fi once.
	if len(plan.body) > 0 || len(plan.deletes) > 0 {
		if err := a.gnmi.SetAP(ctx, plan.body, plan.deletes); err != nil {
			fail(w, http.StatusBadGateway, "The access point rejected the wireless settings: "+err.Error())
			return
		}
		applied = append(applied, plan.steps...)
		for _, change := range plan.ssids {
			a.recordMixed(change)
		}
		a.poller.Refresh()
	}
	b := req.Backup
	var problems []string
	if req.Sections.Radios && b.WiFi7 != nil {
		fallback := 160
		for _, br := range b.Radios {
			if br.Band == "6" {
				fallback = br.Width
			}
		}
		nativeCtx, stop := context.WithTimeout(context.Background(), 90*time.Second)
		if err := a.wifi7.Update(nativeCtx, *b.WiFi7, fallback); err != nil {
			problems = append(problems, "Wi-Fi 7: "+err.Error())
		} else {
			applied = append(applied, "6 GHz Wi-Fi mode")
		}
		stop()
	} else if err := a.ensureWiFi7(); err != nil {
		problems = append(problems, "Wi-Fi 7: "+err.Error())
	}
	if req.Sections.Labels {
		if err := a.cfg.SetLabels(strings.TrimSpace(b.Labels.SiteName), b.Labels.VLANNames); err != nil {
			problems = append(problems, "names: "+err.Error())
		} else {
			applied = append(applied, "names and labels")
		}
	}
	if req.Sections.Time && b.Time != nil {
		if err := saveTimeSettings(ctx, *b.Time, true); err != nil {
			problems = append(problems, "time servers: "+err.Error())
		} else {
			applied = append(applied, "time servers")
		}
	}
	if req.Sections.LLDP && b.LLDP != nil {
		if err := (lldpBackend{path: lldpSettingsFile, run: runVendorTool}).save(ctx, *b.LLDP); err != nil {
			problems = append(problems, "LLDP timing: "+err.Error())
		} else {
			applied = append(applied, "LLDP timing")
		}
	}
	reboot := false
	if plan.management != nil {
		current, _, _ := a.cli.Snapshot()
		if !managementMatches(*plan.management, plan.comm, current) {
			if err := stageManagement(ctx, *plan.management, plan.comm); err != nil {
				problems = append(problems, "management network: "+err.Error())
			} else {
				a.cli.RecordManagement(*plan.management, plan.comm)
				applied = append(applied, "management network (applies after restart)")
				reboot = true
			}
		}
	}
	a.refreshCLI()
	log.Printf("configuration restored by %s: %s", clientIP(r), strings.Join(applied, ", "))
	reply(w, http.StatusOK, map[string]any{"ok": len(problems) == 0, "applied": applied, "problems": problems, "rebootRequired": reboot})
}
