package bacnet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

const DefaultPort = 47808

const (
	bvlcType      = 0x81
	bvlcUnicast   = 0x0A
	bvlcBroadcast = 0x0B

	pduConfirmed = 0x0
	pduSimpleAck = 0x2
	pduComplex   = 0x3
	pduError     = 0x5
	pduReject    = 0x6
	pduAbort     = 0x7

	svcReadProperty         = 12
	svcReadPropertyMultiple = 14
	svcWriteProperty        = 15

	// max-segments "unspecified" with no segmentation; max APDU code 5 = 1476
	// octets, the BACnet/IP maximum. Responses must fit in one datagram.
	maxAPDUCode = 0x05
)

// Error is a BACnet Error-PDU (error class + code) or a Reject/Abort reason.
type Error struct {
	Kind  string // "error", "reject" or "abort"
	Class uint32
	Code  uint32
}

func (e *Error) Error() string {
	if e.Kind == "error" {
		return fmt.Sprintf("bacnet error: class %d code %d", e.Class, e.Code)
	}
	return fmt.Sprintf("bacnet %s: reason %d", e.Kind, e.Code)
}

func frame(npduControl byte, apdu []byte) []byte {
	b := make([]byte, 0, 6+len(apdu))
	b = append(b, bvlcType, bvlcUnicast, 0, 0, 0x01, npduControl)
	b = append(b, apdu...)
	binary.BigEndian.PutUint16(b[2:], uint16(len(b)))
	return b
}

func confirmedRequest(invokeID byte, service byte, body []byte) []byte {
	apdu := append([]byte{pduConfirmed << 4, maxAPDUCode, invokeID, service}, body...)
	return frame(0x04, apdu) // expecting reply
}

// splitResponse strips BVLC and NPDU headers and returns the APDU.
func splitResponse(b []byte) ([]byte, error) {
	if len(b) < 6 || b[0] != bvlcType {
		return nil, errors.New("bacnet: not a BACnet/IP frame")
	}
	switch b[1] {
	case bvlcUnicast, bvlcBroadcast:
		b = b[4:]
	case 0x04: // Forwarded-NPDU carries the original source address
		if len(b) < 10 {
			return nil, errShort
		}
		b = b[10:]
	default:
		return nil, fmt.Errorf("bacnet: unsupported BVLC function 0x%02x", b[1])
	}
	if len(b) < 2 || b[0] != 0x01 {
		return nil, errors.New("bacnet: bad NPDU")
	}
	control := b[1]
	i := 2
	if control&0x80 != 0 {
		return nil, errors.New("bacnet: network-layer message")
	}
	if control&0x20 != 0 { // destination present
		if len(b) < i+3 {
			return nil, errShort
		}
		dlen := int(b[i+2])
		i += 3 + dlen
	}
	if control&0x08 != 0 { // source present
		if len(b) < i+3 {
			return nil, errShort
		}
		slen := int(b[i+2])
		i += 3 + slen
	}
	if control&0x20 != 0 {
		i++ // hop count
	}
	if len(b) < i {
		return nil, errShort
	}
	return b[i:], nil
}

// parseAck checks the APDU for an ack to the given invoke id and returns the
// service-ack payload (nil for SimpleAck).
func parseAck(apdu []byte, invokeID, service byte) ([]byte, bool, error) {
	if len(apdu) < 3 {
		return nil, false, errShort
	}
	kind := apdu[0] >> 4
	switch kind {
	case pduSimpleAck:
		if apdu[1] != invokeID {
			return nil, false, nil
		}
		return nil, true, nil
	case pduComplex:
		if apdu[0]&0x08 != 0 {
			return nil, true, errors.New("bacnet: segmented response not supported")
		}
		if apdu[1] != invokeID {
			return nil, false, nil
		}
		if apdu[2] != service {
			return nil, true, fmt.Errorf("bacnet: ack for service %d, want %d", apdu[2], service)
		}
		return apdu[3:], true, nil
	case pduError:
		if apdu[1] != invokeID {
			return nil, false, nil
		}
		d := &decoder{b: apdu, i: 3}
		var vals []uint32
		for !d.eof() && len(vals) < 2 {
			v, err := d.appValue()
			if err != nil {
				break
			}
			if e, ok := v.(Enumerated); ok {
				vals = append(vals, uint32(e))
			}
		}
		e := &Error{Kind: "error"}
		if len(vals) == 2 {
			e.Class, e.Code = vals[0], vals[1]
		}
		return nil, true, e
	case pduReject, pduAbort:
		if apdu[1] != invokeID {
			return nil, false, nil
		}
		name := "reject"
		if kind == pduAbort {
			name = "abort"
		}
		return nil, true, &Error{Kind: name, Code: uint32(apdu[2])}
	}
	return nil, false, nil
}

// Client talks to a single BACnet/IP device. Requests are serialised; the
// Flexit controller is slow and occasionally drops datagrams, so every
// request is retried.
type Client struct {
	Addr    netip.AddrPort
	Timeout time.Duration
	Retries int

	mu       sync.Mutex
	invokeID byte
}

func NewClient(addr netip.AddrPort) *Client {
	return &Client{Addr: addr, Timeout: 1500 * time.Millisecond, Retries: 3}
}

func (c *Client) request(ctx context.Context, service byte, body []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(c.Addr))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var lastErr error
	buf := make([]byte, 2048)
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.invokeID++
		id := c.invokeID
		if _, err := conn.Write(confirmedRequest(id, service, body)); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(c.Timeout)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetReadDeadline(deadline)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				lastErr = err
				break
			}
			apdu, err := splitResponse(buf[:n])
			if err != nil {
				continue
			}
			payload, matched, err := parseAck(apdu, id, service)
			if !matched {
				continue // stale reply to an earlier attempt
			}
			if err != nil {
				return nil, err
			}
			return append([]byte(nil), payload...), nil
		}
	}
	return nil, fmt.Errorf("bacnet: no response from %s: %w", c.Addr, lastErr)
}

// ReadProperty reads one property and returns its value(s).
func (c *Client) ReadProperty(ctx context.Context, obj ObjectID, prop PropertyID) ([]Value, error) {
	return readProperty(ctx, c.request, obj, prop)
}

type requester func(ctx context.Context, service byte, body []byte) ([]byte, error)

func readPropertyBody(obj ObjectID, prop PropertyID) []byte {
	e := &encoder{}
	e.contextObjectID(0, obj)
	e.contextUnsigned(1, uint32(prop))
	return e.b
}

func readProperty(ctx context.Context, req requester, obj ObjectID, prop PropertyID) ([]Value, error) {
	payload, err := req(ctx, svcReadProperty, readPropertyBody(obj, prop))
	if err != nil {
		return nil, err
	}
	return parseReadPropertyAck(payload)
}

func parseReadPropertyAck(payload []byte) ([]Value, error) {
	d := &decoder{b: payload}
	if _, err := d.contextObjectID(0); err != nil {
		return nil, err
	}
	if _, err := d.contextUnsigned(1); err != nil {
		return nil, err
	}
	if t, err := d.peekTag(); err == nil && t.context && t.number == 2 && !t.opening {
		d.contextUnsigned(2) // array index
	}
	if err := d.expectOpen(3); err != nil {
		return nil, err
	}
	return d.valuesUntilClose(3)
}

// PropertyRef names one property of one object.
type PropertyRef struct {
	Object   ObjectID
	Property PropertyID
}

// Result is the outcome of reading a single property. Err is set when the
// device reported a per-property access error (e.g. unknown object).
type Result struct {
	Values []Value
	Err    *Error
}

// Value returns the first value, or nil.
func (r Result) Value() Value {
	if r.Err != nil || len(r.Values) == 0 {
		return nil
	}
	return r.Values[0]
}

// maxObjectsPerRequest is the starting chunk size for ReadPropertyMultiple.
// The unit does not segment responses; when a chunk's reply would not fit in
// one APDU it aborts, and the chunk is split in half and retried.
const maxObjectsPerRequest = 30

const (
	abortBufferOverflow           = 1
	abortSegmentationNotSupported = 4
)

// ReadMultiple reads many properties using ReadPropertyMultiple, chunked so
// replies need no segmentation.
func (c *Client) ReadMultiple(ctx context.Context, refs []PropertyRef) (map[PropertyRef]Result, error) {
	out := make(map[PropertyRef]Result, len(refs))
	// group by object, preserving order
	var order []ObjectID
	byObj := map[ObjectID][]PropertyID{}
	for _, r := range refs {
		if _, ok := byObj[r.Object]; !ok {
			order = append(order, r.Object)
		}
		byObj[r.Object] = append(byObj[r.Object], r.Property)
	}
	for start := 0; start < len(order); start += maxObjectsPerRequest {
		end := min(start+maxObjectsPerRequest, len(order))
		if err := c.readChunk(ctx, order[start:end], byObj, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (c *Client) readChunk(ctx context.Context, objs []ObjectID, props map[ObjectID][]PropertyID, out map[PropertyRef]Result) error {
	e := &encoder{}
	for _, obj := range objs {
		e.contextObjectID(0, obj)
		e.open(1)
		for _, p := range props[obj] {
			e.contextUnsigned(0, uint32(p))
		}
		e.close(1)
	}
	payload, err := c.request(ctx, svcReadPropertyMultiple, e.b)
	var be *Error
	if errors.As(err, &be) && be.Kind == "abort" && len(objs) > 1 &&
		(be.Code == abortSegmentationNotSupported || be.Code == abortBufferOverflow) {
		half := len(objs) / 2
		if err := c.readChunk(ctx, objs[:half], props, out); err != nil {
			return err
		}
		return c.readChunk(ctx, objs[half:], props, out)
	}
	if err != nil {
		return err
	}
	return parseRPMAck(payload, out)
}

func parseRPMAck(payload []byte, out map[PropertyRef]Result) error {
	d := &decoder{b: payload}
	for !d.eof() {
		obj, err := d.contextObjectID(0)
		if err != nil {
			return err
		}
		if err := d.expectOpen(1); err != nil {
			return err
		}
		for {
			t, err := d.peekTag()
			if err != nil {
				return err
			}
			if t.closing && t.number == 1 {
				d.tag()
				break
			}
			prop, err := d.contextUnsigned(2)
			if err != nil {
				return err
			}
			if t, _ := d.peekTag(); t.context && t.number == 3 && !t.opening {
				d.contextUnsigned(3) // array index
			}
			ref := PropertyRef{obj, PropertyID(prop)}
			t, err = d.tag()
			if err != nil {
				return err
			}
			switch {
			case t.opening && t.number == 4:
				vals, err := d.valuesUntilClose(4)
				if err != nil {
					return err
				}
				out[ref] = Result{Values: vals}
			case t.opening && t.number == 5:
				vals, err := d.valuesUntilClose(5)
				if err != nil {
					return err
				}
				e := &Error{Kind: "error"}
				if len(vals) == 2 {
					c, _ := AsFloat(vals[0])
					k, _ := AsFloat(vals[1])
					e.Class, e.Code = uint32(c), uint32(k)
				}
				out[ref] = Result{Err: e}
			default:
				return fmt.Errorf("bacnet: unexpected tag in RPM result for %v", ref)
			}
		}
	}
	return nil
}

// WriteProperty writes the present value of obj at the given priority
// (0 = no priority, i.e. the device default of 16). Writing Null{}
// relinquishes that priority slot.
func (c *Client) WriteProperty(ctx context.Context, obj ObjectID, prop PropertyID, value Value, priority uint8) error {
	e := &encoder{}
	e.contextObjectID(0, obj)
	e.contextUnsigned(1, uint32(prop))
	e.open(3)
	if err := e.appValue(value); err != nil {
		return err
	}
	e.close(3)
	if priority > 0 {
		e.contextUnsigned(4, uint32(priority))
	}
	_, err := c.request(ctx, svcWriteProperty, e.b)
	return err
}
