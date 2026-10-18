package bacnet

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Captured from a CL3 REL: ReadProperty(device 4194303, object-identifier).
func TestParseReadPropertyAckCaptured(t *testing.T) {
	apdu, err := splitResponse(mustHex(t, "810a0017010030070c0c02000002194b3ec4020000023f"))
	if err != nil {
		t.Fatal(err)
	}
	payload, matched, err := parseAck(apdu, 7, svcReadProperty)
	if !matched || err != nil {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	vals, err := parseReadPropertyAck(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := ObjectID{Type: Device, Instance: 2}
	if len(vals) != 1 || vals[0] != want {
		t.Fatalf("got %v, want %v", vals, want)
	}
}

func TestParseReadPropertyAckString(t *testing.T) {
	apdu, _ := splitResponse(mustHex(t, "810a0022010030070c0c02000002191c3e750e003830303230312d3030303030303f"))
	payload, _, _ := parseAck(apdu, 7, svcReadProperty)
	vals, err := parseReadPropertyAck(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 1 || vals[0] != "800201-000000" {
		t.Fatalf("got %q", vals)
	}
}

// The write body must match the README's verified raw example
// (WriteProperty analogOutput 3 = 100.0 @ 13, and NULL to relinquish).
func TestWriteBodyMatchesReference(t *testing.T) {
	for _, tc := range []struct {
		value Value
		ref   string
	}{
		{float32(100), "810a001a01040244010f0c0040000319553e4442c800003f490d"},
		{Null{}, "810a001601040244010f0c0040000319553e003f490d"},
	} {
		e := &encoder{}
		e.contextObjectID(0, ObjectID{Type: AnalogOutput, Instance: 3})
		e.contextUnsigned(1, uint32(PropPresentValue))
		e.open(3)
		if err := e.appValue(tc.value); err != nil {
			t.Fatal(err)
		}
		e.close(3)
		e.contextUnsigned(4, 13)
		// reference = BVLC(4) + NPDU(2) + APDU header(4) + body
		if want := mustHex(t, tc.ref)[10:]; !bytes.Equal(e.b, want) {
			t.Errorf("%v: body %x, want %x", tc.value, e.b, want)
		}
	}
}

func TestUnsignedEncodingAbove255(t *testing.T) {
	e := &encoder{}
	if err := e.appValue(uint32(360)); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x22, 0x01, 0x68}; !bytes.Equal(e.b, want) {
		t.Fatalf("got %x, want %x", e.b, want)
	}
	d := &decoder{b: e.b}
	v, err := d.appValue()
	if err != nil || v != uint32(360) {
		t.Fatalf("round trip: %v %v", v, err)
	}
}

func TestParseRPMAck(t *testing.T) {
	e := &encoder{}
	// analogInput 1: present-value = 9.5
	e.contextObjectID(0, ObjectID{Type: AnalogInput, Instance: 1})
	e.open(1)
	e.contextUnsigned(2, uint32(PropPresentValue))
	e.open(4)
	e.appValue(float32(9.5))
	e.close(4)
	e.close(1)
	// analogInput 96: unknown object (class 1, code 31)
	e.contextObjectID(0, ObjectID{Type: AnalogInput, Instance: 96})
	e.open(1)
	e.contextUnsigned(2, uint32(PropPresentValue))
	e.open(5)
	e.appValue(Enumerated(1))
	e.appValue(Enumerated(31))
	e.close(5)
	e.close(1)

	out := map[PropertyRef]Result{}
	if err := parseRPMAck(e.b, out); err != nil {
		t.Fatal(err)
	}
	ok := out[PropertyRef{ObjectID{AnalogInput, 1}, PropPresentValue}]
	if ok.Value() != float32(9.5) {
		t.Errorf("value = %v", ok.Value())
	}
	missing := out[PropertyRef{ObjectID{AnalogInput, 96}, PropPresentValue}]
	if missing.Err == nil || missing.Err.Code != 31 {
		t.Errorf("err = %v", missing.Err)
	}
}

func TestParseAbort(t *testing.T) {
	// BVLC + NPDU + Abort (server) invoke 9 reason 4
	apdu, _ := splitResponse(mustHex(t, "810a0009010071090"+"4"))
	_, matched, err := parseAck(apdu, 9, svcReadPropertyMultiple)
	var be *Error
	if !matched || !errors.As(err, &be) || be.Kind != "abort" || be.Code != abortSegmentationNotSupported {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
}

func TestPriorityArrayValues(t *testing.T) {
	e := &encoder{}
	e.open(3)
	for i := 1; i <= 16; i++ {
		if i == 15 {
			e.appValue(float32(40))
		} else {
			e.appValue(Null{})
		}
	}
	e.close(3)
	d := &decoder{b: e.b}
	d.expectOpen(3)
	vals, err := d.valuesUntilClose(3)
	if err != nil || len(vals) != 16 {
		t.Fatalf("vals=%v err=%v", vals, err)
	}
	if _, isNull := vals[12].(Null); !isNull || vals[14] != float32(40) {
		t.Fatalf("unexpected %v", vals)
	}
}
