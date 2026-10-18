package flexit

import (
	"bufio"
	"context"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oskarkook/flexit-tui/internal/bacnet"
)

// commonSubnets are home-router defaults, swept so a unit is found even when
// this machine is not directly on the unit's subnet (VPN, container, a
// second router).
var commonSubnets = []string{
	"192.168.0.0/24", "192.168.1.0/24", "192.168.2.0/24", "192.168.10.0/24",
	"192.168.50.0/24", "192.168.68.0/24", "192.168.86.0/24", "192.168.88.0/24",
	"192.168.100.0/24", "192.168.178.0/24", "10.0.0.0/24", "10.0.1.0/24",
	"10.1.1.0/24", "172.16.0.0/24",
}

// maxSweepBits limits sweeping a local network to at most a /22; larger
// networks only get the /24 around this machine's address.
const maxSweepBits = 22

// Candidates returns the addresses to probe: every host on the local
// networks, on the /24s of known units and ARP neighbours, on common home
// subnets, and on any extra prefixes.
func Candidates(known []netip.Addr, extra []netip.Prefix) []netip.Addr {
	var prefixes []netip.Prefix
	for _, p := range bacnet.LocalPrefixes() {
		if p.Bits() >= maxSweepBits {
			prefixes = append(prefixes, p.Masked())
		} else {
			prefixes = append(prefixes, netip.PrefixFrom(p.Addr(), 24).Masked())
		}
	}
	for _, a := range append(known, arpNeighbours()...) {
		if a.Is4() {
			prefixes = append(prefixes, netip.PrefixFrom(a, 24).Masked())
		}
	}
	for _, s := range commonSubnets {
		prefixes = append(prefixes, netip.MustParsePrefix(s))
	}
	prefixes = append(prefixes, extra...)

	seen := map[netip.Addr]bool{}
	var out []netip.Addr
	add := func(a netip.Addr) {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	// Known units first so they answer early.
	for _, a := range known {
		add(a)
	}
	for _, p := range prefixes {
		p = p.Masked()
		if !p.Addr().Is4() {
			continue
		}
		if p.Bits() >= 31 {
			add(p.Addr())
			continue
		}
		last := lastAddr(p)
		for a := p.Addr().Next(); a.IsValid() && a.Less(last); a = a.Next() {
			add(a)
		}
	}
	return out
}

func lastAddr(p netip.Prefix) netip.Addr {
	a := p.Addr().As4()
	host := 32 - p.Bits()
	for i := 3; i >= 0 && host > 0; i-- {
		n := min(host, 8)
		a[i] |= byte(1<<n - 1)
		host -= n
	}
	return netip.AddrFrom4(a)
}

// arpNeighbours lists IPv4 neighbours from the Linux ARP table; elsewhere it
// returns nothing.
func arpNeighbours() []netip.Addr {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []netip.Addr
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		if a, err := netip.ParseAddr(fields[0]); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// Discover finds units on the network and reads their identity.
func Discover(ctx context.Context, known []netip.Addr, extra []netip.Prefix) ([]Info, error) {
	found, err := bacnet.Discover(ctx, Candidates(known, extra), bacnet.BroadcastAddrs(),
		bacnet.DefaultPort, 1500*time.Millisecond)
	if err != nil && len(found) == 0 {
		return nil, err
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []Info
	)
	for _, f := range found {
		wg.Go(func() {
			info, err := ReadInfo(ctx, bacnet.NewClient(f.Addr), f.DeviceID)
			if err != nil {
				info = Info{Addr: f.Addr, DeviceID: f.DeviceID}
			}
			mu.Lock()
			out = append(out, info)
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, nil
}

// Probe checks a single address (e.g. entered by the user) for a unit.
func Probe(ctx context.Context, addr netip.AddrPort) (Info, error) {
	c := bacnet.NewClient(addr)
	vals, err := c.ReadProperty(ctx, bacnet.ObjectID{Type: bacnet.Device, Instance: bacnet.WildcardInstance},
		bacnet.PropObjectIdentifier)
	if err != nil {
		return Info{}, err
	}
	id := uint32(2)
	if len(vals) > 0 {
		if o, ok := vals[0].(bacnet.ObjectID); ok {
			id = o.Instance
		}
	}
	return ReadInfo(ctx, c, id)
}
