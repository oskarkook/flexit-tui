package flexit

import (
	"net/netip"
	"testing"
)

func TestLastAddr(t *testing.T) {
	for p, want := range map[string]string{
		"192.168.1.0/24": "192.168.1.255",
		"10.0.0.0/22":    "10.0.3.255",
		"10.0.4.0/30":    "10.0.4.3",
	} {
		if got := lastAddr(netip.MustParsePrefix(p)); got.String() != want {
			t.Errorf("%s: got %s, want %s", p, got, want)
		}
	}
}

func TestCandidatesIncludeKnownAndExtra(t *testing.T) {
	known := netip.MustParseAddr("192.168.77.9")
	got := Candidates([]netip.Addr{known}, []netip.Prefix{netip.MustParsePrefix("172.20.5.0/30")})
	if len(got) == 0 || got[0] != known {
		t.Fatalf("known unit should be probed first, got %v", got[:min(3, len(got))])
	}
	has := map[netip.Addr]bool{}
	for _, a := range got {
		if has[a] {
			t.Fatalf("duplicate %s", a)
		}
		has[a] = true
	}
	for _, a := range []string{"192.168.77.1", "192.168.77.254", "172.20.5.1", "172.20.5.2", "192.168.1.50"} {
		if !has[netip.MustParseAddr(a)] {
			t.Errorf("missing %s", a)
		}
	}
	for _, a := range []string{"192.168.77.0", "192.168.77.255", "172.20.5.3"} {
		if has[netip.MustParseAddr(a)] {
			t.Errorf("network/broadcast address %s included", a)
		}
	}
}

func TestModelFromSerial(t *testing.T) {
	if got := modelFromSerial("800201-000000"); got != "CL3 REL" {
		t.Fatalf("got %q", got)
	}
	if got := modelFromSerial("bogus"); got != "" {
		t.Fatalf("got %q", got)
	}
}
