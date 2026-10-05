package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func livePolicyFixture(t *testing.T) (livePolicyBackend, []byte, []byte, string, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "native.conf")
	apConf := filepath.Join(dir, "ap.conf")
	before := []byte(sampleAPConf)
	_ = os.WriteFile(target, before, 0600)
	_ = os.Symlink(target, apConf)
	_ = os.WriteFile(apConf+".enc", []byte("old encrypted configuration"), 0600)
	profile := filepath.Join(dir, "profile111")
	_ = os.Mkdir(profile, 0700)
	_ = os.WriteFile(filepath.Join(profile, "profile.conf"), []byte("VAP_LIST=ath00\nUNRELATED=keep\n"), 0600)
	next, _, err := rewritePolicyFields(before, map[string]map[string]string{"Office": policyFields(testPolicy())})
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "candidate")
	_ = os.WriteFile(candidate, next, 0600)
	b := livePolicyBackend{apConf: apConf, profileRoot: dir, workDir: dir, write: atomicNative, quiet: func(context.Context) error { return nil }, drivers: func(context.Context, string, SSIDPolicy) error { return nil }}
	b.encrypt = func(_ context.Context, plain, enc, check string) error {
		raw, err := os.ReadFile(plain)
		if err != nil {
			return err
		}
		if err = os.WriteFile(enc, append([]byte("encrypted:"), raw...), 0600); err != nil {
			return err
		}
		return os.WriteFile(check, raw, 0600)
	}
	return b, before, next, candidate, target
}

func TestLivePolicyPreservesNativeSymlinkAndOtherProfileFields(t *testing.T) {
	b, before, next, candidate, target := livePolicyFixture(t)
	if err := b.apply(context.Background(), before, next, []string{"111"}, candidate); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(b.apConf); err != nil || link != target {
		t.Fatal("native symlink replaced", err)
	}
	raw, _ := os.ReadFile(target)
	if !bytes.Equal(raw, next) {
		t.Fatal("native configuration not saved")
	}
	profile, _ := os.ReadFile(filepath.Join(b.profileRoot, "profile111", "profile.conf"))
	if !strings.Contains(string(profile), "UNRELATED=keep\n") || !strings.Contains(string(profile), "ENA_ASSOC_LIMIT=1\n") {
		t.Fatal("unrelated native profile field changed")
	}
	list, _ := os.ReadFile(filepath.Join(b.profileRoot, "profile111", "mac_acl.conf"))
	if string(list) != "02:00:00:46:00:01\n" {
		t.Fatal("MAC list mismatch")
	}
}

func TestLivePolicyRollsBackEncryptedAndProfileFilesOnDriverFailure(t *testing.T) {
	b, before, next, candidate, target := livePolicyFixture(t)
	calls := []string{}
	b.drivers = func(_ context.Context, _ string, p SSIDPolicy) error {
		calls = append(calls, p.MACFilter.Mode)
		if p.MACFilter.Mode == "deny" {
			return errors.New("driver failure")
		}
		return nil
	}
	if err := b.apply(context.Background(), before, next, []string{"111"}, candidate); err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatal("failure not restored", err)
	}
	raw, _ := os.ReadFile(target)
	enc, _ := os.ReadFile(b.apConf + ".enc")
	profile, _ := os.ReadFile(filepath.Join(b.profileRoot, "profile111", "profile.conf"))
	if !bytes.Equal(raw, before) || string(enc) != "old encrypted configuration" || string(profile) != "VAP_LIST=ath00\nUNRELATED=keep\n" || strings.Join(calls, ",") != "deny,off" {
		t.Fatal("rollback incomplete", calls)
	}
	if _, err := os.Stat(filepath.Join(b.profileRoot, "profile111", "mac_acl.conf")); !os.IsNotExist(err) {
		t.Fatal("rollback retained new ACL file")
	}
}

func TestLivePolicyRejectsBadEncryptionAndConcurrentNativeWriter(t *testing.T) {
	for _, cause := range []string{"encryption", "concurrent"} {
		t.Run(cause, func(t *testing.T) {
			b, before, next, candidate, target := livePolicyFixture(t)
			encrypt := b.encrypt
			b.encrypt = func(ctx context.Context, plain, enc, check string) error {
				if err := encrypt(ctx, plain, enc, check); err != nil {
					return err
				}
				if cause == "encryption" {
					return os.WriteFile(check, []byte("corrupt"), 0600)
				}
				return os.WriteFile(target, []byte(strings.Replace(sampleAPConf, "X=1", "X=2", 1)), 0600)
			}
			b.drivers = func(context.Context, string, SSIDPolicy) error {
				t.Fatal("unverified files reached driver")
				return nil
			}
			if err := b.apply(context.Background(), before, next, []string{"111"}, candidate); err == nil {
				t.Fatal("bad native commit accepted")
			}
			enc, _ := os.ReadFile(b.apConf + ".enc")
			if string(enc) != "old encrypted configuration" {
				t.Fatal("unverified encrypted configuration published")
			}
		})
	}
}
