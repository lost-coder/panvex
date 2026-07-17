#!/usr/bin/env bash
# Unit test for is_ip_address() in ../install.sh.
#
# Extracts the function body straight out of install.sh (rather than
# duplicating it here) so the test always exercises the real
# implementation, then runs it against a table of IPv4/IPv6/hostname
# cases.
set -euo pipefail

src="$(dirname "$0")/../install.sh"
eval "$(sed -n '/^is_ip_address()/,/^}/p' "$src")"

pass=0
fail=0

expect() { # expect <0|1> <addr>
  local want="$1" addr="$2" rc
  if is_ip_address "$addr"; then rc=0; else rc=1; fi
  if [[ "$rc" == "$want" ]]; then
    pass=$((pass + 1))
  else
    fail=$((fail + 1))
    echo "FAIL: $addr rc=$rc want=$want"
  fi
}

expect 0 "203.0.113.1"
expect 0 "0.0.0.0"
expect 0 "255.255.255.255"
expect 0 "2001:db8::1"
expect 0 "::1"
expect 1 "example.com"
expect 1 "not-an-ip"
expect 1 "256.1.1.1"
expect 1 "1.2.3"
expect 1 "1.2.3.4.5"
expect 1 ""

echo "pass=$pass fail=$fail"
[[ "$fail" == 0 ]]
