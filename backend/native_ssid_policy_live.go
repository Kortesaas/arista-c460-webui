package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Admission controls have live driver setters. A generic VAP MOD also restarts
// Wi-Fi and can trip this firmware's beacon-loss watchdog after repeated saves.
// Keep the native profile and TPM-encrypted boot copy consistent, then use the
// vendor's MAC helper and maxsta setter without rebuilding any radio or VAP.
type livePolicyBackend struct {
	apConf, profileRoot, workDir string
	encrypt                      func(context.Context, string, string, string) error
	drivers                      func(context.Context, string, SSIDPolicy) error
	write                        func(string, []byte, os.FileMode) error
	quiet                        func(context.Context) error
}

func applyLivePolicy(ctx context.Context, before, next []byte, profiles []string, candidate string) error {
	b := livePolicyBackend{apConf: nativeAPConf, profileRoot: "/tmp", workDir: "/opt/c460-webui", encrypt: encryptPolicyCopy, drivers: applyPolicyDrivers, write: atomicNative, quiet: waitNativeQuiet}
	return b.apply(ctx, before, next, profiles, candidate)
}

func encryptPolicyCopy(ctx context.Context, candidate, encrypted, checked string) error {
	if err := vendorShell(ctx, "/bin/sh", "/opt/sensor/scripts/encrypt_secret.sh", "-i", candidate, "-o", encrypted).Run(); err != nil {
		return fmt.Errorf("Encrypt native client access settings: %w", err)
	}
	if err := vendorShell(ctx, "/bin/sh", "/opt/sensor/scripts/encrypt_secret.sh", "-d", "-i", encrypted, "-o", checked).Run(); err != nil {
		return fmt.Errorf("Verify encrypted client access settings: %w", err)
	}
	return nil
}

func (b livePolicyBackend) apply(ctx context.Context, before, next []byte, profiles []string, candidate string) error {
	sections, err := policySections(next)
	if err != nil {
		return err
	}
	previous, err := policySections(before)
	if err != nil {
		return err
	}
	oldPolicies := map[string]SSIDPolicy{}
	for _, section := range previous {
		if !slices.Contains(profiles, section.profile) {
			continue
		}
		policy, err := policyFromFields(section.fields)
		if err != nil {
			return err
		}
		oldPolicies[section.profile] = policy
	}
	var writes []nativeWrite
	plain, err := prepareNative(b.apConf, nil)
	if err != nil {
		return err
	}
	if !bytes.Equal(plain.before, before) {
		return errors.New("Native configuration changed before client access commit")
	}
	plain.path, err = filepath.EvalSymlinks(b.apConf)
	if err != nil {
		return err
	}
	plain.after = next
	enc, err := prepareNative(b.apConf+".enc", nil)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(b.workDir, ".policy-encrypt-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	encrypted, checked := filepath.Join(dir, "ap.conf.enc"), filepath.Join(dir, "ap.conf.check")
	if err = b.encrypt(ctx, candidate, encrypted, checked); err != nil {
		return err
	}
	decoded, err := os.ReadFile(checked)
	if err != nil || !bytes.Equal(decoded, next) {
		return errors.New("Encrypted client access settings failed verification")
	}
	enc.after, err = os.ReadFile(encrypted)
	if err != nil || len(enc.after) == 0 {
		return errors.New("Encrypted client access settings are empty")
	}
	writes = append(writes, enc, plain)
	policies := map[string]SSIDPolicy{}
	for _, section := range sections {
		if !slices.Contains(profiles, section.profile) {
			continue
		}
		policy, err := policyFromFields(section.fields)
		if err != nil {
			return err
		}
		policies[section.profile] = policy
		profile, err := prepareNative(filepath.Join(b.profileRoot, "profile"+section.profile, "profile.conf"), nil)
		if err != nil {
			return err
		}
		if len(profile.before) == 0 {
			return errors.New("Native wireless profile is not ready")
		}
		profile.after = profile.before
		fields := policyFields(policy)
		for _, pair := range [][2]string{{"MAC_ACL_ENABLED", "MAC_ACL_ENABLED"}, {"MAC_ACL_OPERATION", "MAC_ACL_OPERATION"}, {"MAC_ACL_ACTION", "MAC_ACL_OPERATION"}, {"MAC_ACL_LIST", "MAC_ACL_LIST"}, {"ENA_ASSOC_LIMIT", "ENABLE_LIMIT_ON_ASSOC"}, {"ASSOC_LIMIT", "ASSOC_LIMIT"}} {
			profile.after = nativeValue(profile.after, pair[0], fields[pair[1]])
		}
		list, err := prepareNative(filepath.Join(b.profileRoot, "profile"+section.profile, "mac_acl.conf"), nil)
		if err != nil {
			return err
		}
		list.after = []byte{}
		list.mode = 0600
		if policy.MACFilter.Mode != "off" {
			list.after = []byte(strings.Join(policy.MACFilter.Addresses, "\n") + "\n")
		}
		writes = append(writes, profile, list)
	}
	// Detect vendor writers during TPM work before publishing any file.
	if err = b.quiet(ctx); err != nil {
		return err
	}
	if target, err := filepath.EvalSymlinks(b.apConf); err != nil || target != plain.path {
		return errors.New("Native configuration target changed during planning; retry")
	}
	for _, write := range writes {
		raw, readErr := os.ReadFile(write.path)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if !bytes.Equal(raw, write.before) {
			return errors.New("Native client access files changed during planning; retry")
		}
	}
	rollback := func(cause error) error {
		var problems []string
		for i := len(writes) - 1; i >= 0; i-- {
			write := writes[i]
			if write.existed {
				err = b.write(write.path, write.before, write.mode)
			} else {
				err = os.Remove(write.path)
				if os.IsNotExist(err) {
					err = nil
				}
			}
			if err != nil {
				problems = append(problems, err.Error())
			}
		}
		restoreCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		for profile, policy := range oldPolicies {
			if err := b.drivers(restoreCtx, profile, policy); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("%w; native client access rollback failed: %s", cause, strings.Join(problems, "; "))
		}
		return fmt.Errorf("%w; previous native client access restored", cause)
	}
	for _, write := range writes {
		if err = b.write(write.path, write.after, write.mode); err != nil {
			return rollback(err)
		}
	}
	for profile, policy := range policies {
		if err = b.drivers(ctx, profile, policy); err != nil {
			return rollback(err)
		}
	}
	return nil
}

func applyPolicyDrivers(ctx context.Context, profile string, policy SSIDPolicy) error {
	raw, err := os.ReadFile("/tmp/profile" + profile + "/profile.conf")
	if err != nil {
		return err
	}
	interfaces := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if list, ok := vapValue(line, "VAP_LIST"); ok {
			for _, iface := range strings.Split(list, ",") {
				iface, _, _ = strings.Cut(strings.TrimSpace(iface), ".")
				if !validPolicyInterface.MatchString(iface) {
					return errors.New("Invalid native policy interface")
				}
				if policyInterfaceUp(iface) {
					interfaces = append(interfaces, iface)
				}
			}
		}
	}
	macChanged := false
	for _, iface := range interfaces {
		mode, modeErr := driverPolicyInt(ctx, iface, "get_maccmd")
		out, listErr := runVendorTool(ctx, "/sbin/iwpriv", iface, "getmac")
		addresses, parseErr := parseDriverPolicyMACs(out)
		wanted := policy.MACFilter.Addresses
		if policy.MACFilter.Mode == "off" {
			wanted = []string{}
		}
		modeValue, _ := strconv.Atoi(policyFields(policy)["MAC_ACL_OPERATION"])
		if modeErr != nil || listErr != nil || parseErr != nil || *mode != modeValue || !slices.Equal(addresses, wanted) {
			macChanged = true
		}
	}
	if macChanged {
		for _, iface := range interfaces {
			// Force the helper's full-list branch; avoid relying on a stale delta file.
			if _, err = runVendorTool(ctx, "/sbin/iwpriv", iface, "maccmd", "3"); err != nil {
				return err
			}
			if _, err = runVendorTool(ctx, "/sbin/iwpriv", iface, "maccmd", "0"); err != nil {
				return err
			}
		}
		fields := policyFields(policy)
		if err = vendorShell(ctx, "/bin/sh", "/opt/ap/handle_mac_filter.sh", profile, fields["MAC_ACL_ENABLED"], fields["MAC_ACL_OPERATION"]).Run(); err != nil {
			return err
		}
	}
	fields := policyFields(policy)
	for _, iface := range interfaces {
		if _, err = runVendorTool(ctx, "/sbin/iwpriv", iface, "maxsta", fields["ASSOC_LIMIT"]); err != nil {
			return err
		}
	}
	return nil
}
