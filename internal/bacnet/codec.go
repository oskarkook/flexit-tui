// Package bacnet implements the small subset of BACnet/IP needed to talk to a
// Flexit Nordic unit: ReadProperty, ReadPropertyMultiple and WriteProperty over
// unicast UDP.
package bacnet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type ObjectType uint16

const (
	AnalogInput          ObjectType = 0
	AnalogOutput         ObjectType = 1
	AnalogValue          ObjectType = 2
	BinaryInput          ObjectType = 3
	BinaryOutput         ObjectType = 4
	BinaryValue          ObjectType = 5
	Device               ObjectType = 8
	MultiStateValue      ObjectType = 19
	PositiveIntegerValue ObjectType = 48
)

var objectTypeNames = map[ObjectType]string{
	AnalogInput:          "analogInput",
	AnalogOutput:         "analogOutput",
	AnalogValue:          "analogValue",
	BinaryInput:          "binaryInput",
	BinaryOutput:         "binaryOutput",
	BinaryValue:          "binaryValue",
	Device:               "device",
	MultiStateValue:      "multiStateValue",
	PositiveIntegerValue: "positiveIntegerValue",
}

func (t ObjectType) String() string {
	if n, ok := objectTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("objectType(%d)", uint16(t))
}

// WildcardInstance addresses "whatever device answers" in a device object
// identifier. The Flexit honours it, which is what makes discovery possible
// without knowing the device id.
const WildcardInstance = 0x3FFFFF

type ObjectID struct {
	Type     ObjectType
	Instance uint32
}

func (o ObjectID) String() string { return fmt.Sprintf("%s %d", o.Type, o.Instance) }

func (o ObjectID) encode() uint32 { return uint32(o.Type)<<22 | o.Instance&0x3FFFFF }

func decodeObjectID(v uint32) ObjectID {
	return ObjectID{Type: ObjectType(v >> 22), Instance: v & 0x3FFFFF}
}

type PropertyID uint32

const (
	PropDescription        PropertyID = 28
	PropFirmwareRevision   PropertyID = 44
	PropModelName          PropertyID = 70
	PropApplicationVersion PropertyID = 12
	PropObjectIdentifier   PropertyID = 75
	PropObjectName         PropertyID = 77
	PropPresentValue       PropertyID = 85
	PropPriorityArray      PropertyID = 87
	PropRelinquishDefault  PropertyID = 104
	PropVendorName         PropertyID = 121
)

// Application tag numbers.
const (
	tagNull       = 0
	tagBoolean    = 1
	tagUnsigned   = 2
	tagSigned     = 3
	tagReal       = 4
	tagDouble     = 5
	tagOctetStr   = 6
	tagCharStr    = 7
	tagBitString  = 8
	tagEnumerated = 9
	tagDate       = 10
	tagTime       = 11
	tagObjectID   = 12
)

// Null is the decoded value of a BACnet NULL; writing it relinquishes a
// command priority.
type Null struct{}

// Enumerated distinguishes enumerated values (binary states, units) from
// plain unsigned integers.
type Enumerated uint32

// Value is one decoded application-tagged value: Null, bool, uint32, int32,
// float32, float64, []byte, string, Enumerated or ObjectID.
type Value any

// AsFloat converts any numeric BACnet value to float64.
func AsFloat(v Value) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case uint32:
		return float64(x), true
	case int32:
		return float64(x), true
	case Enumerated:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// ---------------------------------------------------------------- encoding

type encoder struct{ b []byte }

func (e *encoder) tag(number byte, context bool, length int) {
	first := number << 4
	if context {
		first |= 0x08
	}
	switch {
	case length < 5:
		e.b = append(e.b, first|byte(length))
	case length < 254:
		e.b = append(e.b, first|5, byte(length))
	default:
		e.b = append(e.b, first|5, 254, byte(length>>8), byte(length))
	}
}

func (e *encoder) open(number byte)  { e.b = append(e.b, number<<4|0x0E) }
func (e *encoder) close(number byte) { e.b = append(e.b, number<<4|0x0F) }

func minimalUnsigned(v uint32) []byte {
	switch {
	case v <= 0xFF:
		return []byte{byte(v)}
	case v <= 0xFFFF:
		return []byte{byte(v >> 8), byte(v)}
	case v <= 0xFFFFFF:
		return []byte{byte(v >> 16), byte(v >> 8), byte(v)}
	}
	return binary.BigEndian.AppendUint32(nil, v)
}

func (e *encoder) contextUnsigned(number byte, v uint32) {
	data := minimalUnsigned(v)
	e.tag(number, true, len(data))
	e.b = append(e.b, data...)
}

func (e *encoder) contextObjectID(number byte, o ObjectID) {
	e.tag(number, true, 4)
	e.b = binary.BigEndian.AppendUint32(e.b, o.encode())
}

func (e *encoder) appValue(v Value) error {
	switch x := v.(type) {
	case Null, nil:
		e.tag(tagNull, false, 0)
	case bool:
		if x {
			e.tag(tagBoolean, false, 1)
		} else {
			e.tag(tagBoolean, false, 0)
		}
	case uint32:
		data := minimalUnsigned(x)
		e.tag(tagUnsigned, false, len(data))
		e.b = append(e.b, data...)
	case Enumerated:
		data := minimalUnsigned(uint32(x))
		e.tag(tagEnumerated, false, len(data))
		e.b = append(e.b, data...)
	case float32:
		e.tag(tagReal, false, 4)
		e.b = binary.BigEndian.AppendUint32(e.b, math.Float32bits(x))
	default:
		return fmt.Errorf("bacnet: cannot encode %T", v)
	}
	return nil
}

// ---------------------------------------------------------------- decoding

var errShort = errors.New("bacnet: truncated data")

type tagHeader struct {
	number  byte
	context bool
	length  int // or, for opening/closing tags, 6/7
	opening bool
	closing bool
}

type decoder struct {
	b []byte
	i int
}

func (d *decoder) eof() bool { return d.i >= len(d.b) }

func (d *decoder) byte() (byte, error) {
	if d.i >= len(d.b) {
		return 0, errShort
	}
	d.i++
	return d.b[d.i-1], nil
}

func (d *decoder) bytes(n int) ([]byte, error) {
	if n < 0 || d.i+n > len(d.b) {
		return nil, errShort
	}
	d.i += n
	return d.b[d.i-n : d.i], nil
}

func (d *decoder) peekTag() (tagHeader, error) {
	save := d.i
	t, err := d.tag()
	d.i = save
	return t, err
}

func (d *decoder) tag() (tagHeader, error) {
	first, err := d.byte()
	if err != nil {
		return tagHeader{}, err
	}
	t := tagHeader{number: first >> 4, context: first&0x08 != 0}
	if t.number == 0x0F {
		n, err := d.byte()
		if err != nil {
			return t, err
		}
		t.number = n
	}
	lvt := int(first & 0x07)
	if t.context && lvt == 6 {
		t.opening = true
		return t, nil
	}
	if t.context && lvt == 7 {
		t.closing = true
		return t, nil
	}
	if lvt == 5 {
		n, err := d.byte()
		if err != nil {
			return t, err
		}
		switch n {
		case 254:
			b, err := d.bytes(2)
			if err != nil {
				return t, err
			}
			lvt = int(binary.BigEndian.Uint16(b))
		case 255:
			b, err := d.bytes(4)
			if err != nil {
				return t, err
			}
			lvt = int(binary.BigEndian.Uint32(b))
		default:
			lvt = int(n)
		}
	}
	t.length = lvt
	return t, nil
}

func (d *decoder) expectOpen(number byte) error {
	t, err := d.tag()
	if err != nil {
		return err
	}
	if !t.opening || t.number != number {
		return fmt.Errorf("bacnet: expected opening tag %d at %d", number, d.i-1)
	}
	return nil
}

func (d *decoder) expectClose(number byte) error {
	t, err := d.tag()
	if err != nil {
		return err
	}
	if !t.closing || t.number != number {
		return fmt.Errorf("bacnet: expected closing tag %d at %d", number, d.i-1)
	}
	return nil
}

func decodeUnsigned(b []byte) uint32 {
	var v uint32
	for _, c := range b {
		v = v<<8 | uint32(c)
	}
	return v
}

func (d *decoder) contextUnsigned(number byte) (uint32, error) {
	t, err := d.tag()
	if err != nil {
		return 0, err
	}
	if !t.context || t.number != number {
		return 0, fmt.Errorf("bacnet: expected context tag %d", number)
	}
	b, err := d.bytes(t.length)
	if err != nil {
		return 0, err
	}
	return decodeUnsigned(b), nil
}

func (d *decoder) contextObjectID(number byte) (ObjectID, error) {
	v, err := d.contextUnsigned(number)
	return decodeObjectID(v), err
}

// appValue decodes one application-tagged value.
func (d *decoder) appValue() (Value, error) {
	t, err := d.tag()
	if err != nil {
		return nil, err
	}
	if t.context {
		return nil, fmt.Errorf("bacnet: expected application tag, got context tag %d", t.number)
	}
	if t.number == tagBoolean {
		return t.length != 0, nil
	}
	data, err := d.bytes(t.length)
	if err != nil {
		return nil, err
	}
	switch t.number {
	case tagNull:
		return Null{}, nil
	case tagUnsigned:
		return decodeUnsigned(data), nil
	case tagSigned:
		v := int32(0)
		if len(data) > 0 && data[0]&0x80 != 0 {
			v = -1
		}
		for _, c := range data {
			v = v<<8 | int32(c)
		}
		return v, nil
	case tagReal:
		if len(data) != 4 {
			return nil, fmt.Errorf("bacnet: real of length %d", len(data))
		}
		return math.Float32frombits(binary.BigEndian.Uint32(data)), nil
	case tagDouble:
		if len(data) != 8 {
			return nil, fmt.Errorf("bacnet: double of length %d", len(data))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data)), nil
	case tagCharStr:
		if len(data) == 0 {
			return "", nil
		}
		// 0 = UTF-8/ANSI X3.4; other charsets are rare and rendered as-is.
		return string(data[1:]), nil
	case tagEnumerated:
		return Enumerated(decodeUnsigned(data)), nil
	case tagObjectID:
		return decodeObjectID(decodeUnsigned(data)), nil
	default:
		return append([]byte(nil), data...), nil
	}
}

// valuesUntilClose decodes application values up to (and consuming) the
// closing tag with the given number. Most properties hold a single value;
// arrays such as the priority array hold several.
func (d *decoder) valuesUntilClose(number byte) ([]Value, error) {
	var out []Value
	for {
		t, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if t.closing && t.number == number {
			d.tag()
			return out, nil
		}
		v, err := d.appValue()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}
