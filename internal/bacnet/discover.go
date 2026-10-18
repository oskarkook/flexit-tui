package bacnet

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Found is a device that answered a discovery probe.
type Found struct {
	Addr     netip.AddrPort
	DeviceID uint32
}

// The Flexit GO app discovers units with an unconfirmed private transfer
// (Siemens vendor 7, service 515). Units that answer it reply with service 516.
// The parameters were captured from the app (see flexit_bacnet).
var siemensDiscoveryParams = []byte{
	0x80, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0x08, 0x64, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72,
	0x00, 0x00, 0x00, 0x00, 0x0c, 0x00, 0x01, 0x0b, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x0b, 0x00,
	0x02, 0x00, 0x00, 0x00, 0x2e, 0x41, 0x42, 0x54, 0x4d, 0x6f, 0x62, 0x69, 0x6c, 0x65, 0x3a, 0x38,
	0x34, 0x33, 0x30, 0x33, 0x64, 0x32, 0x64, 0x2d, 0x30, 0x34, 0x39, 0x37, 0x2d, 0x34, 0x65, 0x33,
	0x62, 0x2d, 0x62, 0x63, 0x38, 0x31, 0x2d, 0x37, 0x65, 0x36, 0x65, 0x62, 0x62, 0x31, 0x31, 0x65,
	0x64, 0x62, 0x38, 0x0b, 0x00, 0x03, 0x00, 0x00, 0x00, 0x0c, 0x3f, 0x44, 0x65, 0x76, 0x69, 0x63,
	0x65, 0x73, 0x3d, 0x41, 0x6c, 0x6c, 0x00, 0x00,
}

func siemensDiscoveryFrame() []byte {
	e := &encoder{b: []byte{0x10, 0x04}} // unconfirmed request, private transfer
	e.contextUnsigned(0, 7)
	e.tag(1, true, 2)
	e.b = append(e.b, 0x02, 0x03) // service 515
	e.open(2)
	e.b = append(e.b, siemensDiscoveryParams...)
	e.close(2)
	b := frame(0x00, e.b)
	b[1] = bvlcBroadcast
	return b
}

// sweepBatch is the number of sweep targets per socket. Probes to absent
// hosts on the local subnet wait in the kernel's ARP queue and are charged
// to the sending socket's buffer; with too many on one socket, sends block
// for seconds until ARP gives up. Small per-socket batches sent concurrently
// avoid that.
const sweepBatch = 64

// discoveryInvokeID tags sweep probes so replies can be told apart from
// unrelated traffic.
const discoveryInvokeID = 0xD5

// Discover probes every target address with a unicast ReadProperty of the
// wildcard device's Object_Identifier, and additionally broadcasts the same
// probe plus the Flexit app's proprietary discovery frame on each broadcast
// address. Standard Who-Is is not used: the Flexit does not answer it.
//
// Every probe is sent twice to cover the unit's occasional dropped datagrams.
func Discover(ctx context.Context, targets []netip.Addr, broadcasts []netip.Addr, port uint16, wait time.Duration) ([]Found, error) {
	probe := confirmedRequest(discoveryInvokeID, svcReadProperty,
		readPropertyBody(ObjectID{Device, WildcardInstance}, PropObjectIdentifier))
	broadcastProbe := append([]byte(nil), probe...)
	broadcastProbe[1] = bvlcBroadcast
	siemens := siemensDiscoveryFrame()

	var (
		mu                sync.Mutex
		found             = map[netip.Addr]Found{}
		siemensResponders = map[netip.Addr]bool{}
		readers           sync.WaitGroup
		senders           sync.WaitGroup
		conns             []*net.UDPConn
	)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	receive := func(conn *net.UDPConn) {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			src := from.Addr().Unmap()
			apdu, err := splitResponse(buf[:n])
			if err != nil || len(apdu) < 2 {
				continue
			}
			if apdu[0]>>4 == 0x1 { // unconfirmed request: Siemens identification (516)?
				if len(apdu) > 6 && apdu[1] == 0x04 && apdu[3] == 7 {
					mu.Lock()
					siemensResponders[src] = true
					mu.Unlock()
				}
				continue
			}
			payload, matched, err := parseAck(apdu, discoveryInvokeID, svcReadProperty)
			if !matched || err != nil {
				continue
			}
			vals, err := parseReadPropertyAck(payload)
			if err != nil || len(vals) == 0 {
				continue
			}
			if id, ok := vals[0].(ObjectID); ok && id.Type == Device {
				mu.Lock()
				found[src] = Found{Addr: netip.AddrPortFrom(src, port), DeviceID: id.Instance}
				mu.Unlock()
			}
		}
	}

	open := func() (*net.UDPConn, error) {
		conn, err := listenBroadcast()
		if err != nil {
			return nil, err
		}
		conns = append(conns, conn)
		readers.Go(func() { receive(conn) })
		return conn, nil
	}

	bcast, err := open()
	if err != nil {
		return nil, err
	}
	send := func(conn *net.UDPConn, b []byte, to netip.Addr) {
		conn.SetWriteDeadline(time.Now().Add(wait))
		conn.WriteToUDPAddrPort(b, netip.AddrPortFrom(to, port))
	}

	for i := 0; i < len(targets); i += sweepBatch {
		batch := targets[i:min(i+sweepBatch, len(targets))]
		conn, err := open()
		if err != nil {
			return nil, err
		}
		// Stagger batches so the sweep is spread over a few hundred
		// milliseconds rather than one burst.
		delay := time.Duration(i/sweepBatch) * 4 * time.Millisecond
		senders.Go(func() {
			for round := range 2 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay + time.Duration(round)*wait/2):
				}
				for _, t := range batch {
					send(conn, probe, t)
				}
			}
		})
	}
	for round := range 2 {
		for _, b := range broadcasts {
			send(bcast, broadcastProbe, b)
			send(bcast, siemens, b)
		}
		if round == 0 {
			time.Sleep(wait / 2)
		}
	}
	senders.Wait()
	select {
	case <-ctx.Done():
	case <-time.After(wait / 2):
	}

	// Units that only answered the proprietary discovery still need their
	// device id; ask them directly.
	for range 2 {
		mu.Lock()
		var pending []netip.Addr
		for ip := range siemensResponders {
			if _, ok := found[ip]; !ok {
				pending = append(pending, ip)
			}
		}
		mu.Unlock()
		if len(pending) == 0 {
			break
		}
		for _, ip := range pending {
			send(bcast, probe, ip)
		}
		time.Sleep(wait / 4)
	}

	for _, c := range conns {
		c.SetReadDeadline(time.Now())
	}
	readers.Wait()

	out := make([]Found, 0, len(found))
	for _, f := range found {
		out = append(out, f)
	}
	return out, ctx.Err()
}

// BroadcastAddrs returns the limited broadcast address plus each local IPv4
// interface's directed broadcast address.
func BroadcastAddrs() []netip.Addr {
	out := []netip.Addr{netip.AddrFrom4([4]byte{255, 255, 255, 255})}
	for _, p := range LocalPrefixes() {
		a := p.Masked().Addr().As4()
		mask := ^uint32(0) >> p.Bits()
		v := binary.BigEndian.Uint32(a[:]) | mask
		binary.BigEndian.PutUint32(a[:], v)
		out = append(out, netip.AddrFrom4(a))
	}
	return out
}

// LocalPrefixes returns the IPv4 addresses (with prefix length) of all up,
// non-loopback interfaces.
func LocalPrefixes() []netip.Prefix {
	var out []netip.Prefix
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP)
			if !ok || !ip.Unmap().Is4() {
				continue
			}
			bits, _ := ipn.Mask.Size()
			out = append(out, netip.PrefixFrom(ip.Unmap(), bits))
		}
	}
	return out
}
