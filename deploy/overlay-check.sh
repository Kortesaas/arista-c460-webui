#!/bin/sh
# Pre-reboot safety check for the C-460 (firmware 18.2.x).
#
# At boot, /etc/rc.d/fs_init runs the firmware's trust checks
# (/opt/sensor/scripts/pre_overlay_check.sh and post_overlay_check.sh). If they
# fail, the AP deletes the whole writable layer (/overlay/upper) and boots the
# other firmware partition. This script mirrors those checks read-only, so a
# change can be verified before the next reboot. Exit 0 = the next boot keeps
# the writable layer.

UP=/overlay/upper
fail=0
tmp=/tmp/overlay-check.$$
trap 'rm -f "$tmp"' EXIT

# 1. No regular files (symlinks are fine) in protected directories.
protected="/usr /bin /etc /opt/lib /opt/keys /opt/dhclient /opt/udhcpc /opt/scripts /opt/sensor/scripts /opt/init.d /lib /sbin /lib64"
exempt='^/overlay/upper(/etc/resolv\.conf|/etc/profile|/sbin/csr8x11-a12-bt4\.2-patch\.psr|/usr/local/etc/|/lib/firmware/)'
for d in $protected; do
	[ -e "$UP$d" ] && find "$UP$d" -type f
done | grep -v -E "$exempt" >"$tmp"
if [ -s "$tmp" ]; then
	echo "FAIL: regular files in firmware-protected directories (remove them or replace with symlinks):"
	sed 's#^/overlay/upper#  #' "$tmp"
	fail=1
fi

# 2. Files measured into TPM PCR 7 must be unchanged.
for f in /opt/passwd /opt/group /opt/shells /opt/sensor/sensor-shell /opt/sensor/sensor.md5; do
	if [ -e "$UP$f" ] && ! cmp -s "$UP$f" "/rom$f"; then
		echo "FAIL: $f differs from the firmware image"
		fail=1
	fi
done

# 3. Vendor files listed in sensor.md5 must be unchanged.
(cd / && md5sum -c /opt/sensor/sensor.md5 2>&1) | grep -v ': OK$' >"$tmp"
if [ -s "$tmp" ]; then
	echo "FAIL: vendor files modified (md5_verify):"
	sed 's/^/  /' "$tmp"
	fail=1
fi

if [ "$fail" = 0 ]; then
	echo "OK: the writable layer passes the firmware's boot-time trust checks"
fi
exit "$fail"
