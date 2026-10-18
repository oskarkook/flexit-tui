package flexit

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/oskarkook/flexit-tui/internal/bacnet"
)

// Info identifies a unit.
type Info struct {
	Addr            netip.AddrPort
	DeviceID        uint32
	ObjectName      string // e.g. HvacFnct21y_A
	Serial          string // e.g. 800201-000000
	Model           string // e.g. CL3 REL, derived from the serial
	Firmware        string
	AppVersion      string
	Vendor          string
	ControllerModel string
}

// Key identifies the unit across IP address changes.
func (i Info) Key() string {
	if i.Serial != "" {
		return i.Serial
	}
	return i.Addr.String()
}

// Title is a short human-readable name.
func (i Info) Title() string {
	name := "Flexit Nordic"
	if i.Model != "" {
		name += " " + i.Model
	}
	return name
}

// nordicModels maps the first six digits of the serial number to a model.
var nordicModels = map[int]string{
	800111: "S2 REL", 800121: "S3 REL", 800110: "S2 RER", 800120: "S3 RER",
	800221: "CL4 REL", 800220: "CL4 RER", 800130: "S4 RER", 800131: "S4 REL",
	800210: "CL2 RER", 800211: "CL2 REL", 800200: "CL3 RER", 800201: "CL3 REL",
	800300: "KS3 RER", 800301: "KS3 REL",
}

func modelFromSerial(serial string) string {
	if len(serial) < 6 {
		return ""
	}
	n, err := strconv.Atoi(serial[:6])
	if err != nil {
		return ""
	}
	return nordicModels[n]
}

// ReadInfo reads the device object's identification properties.
func ReadInfo(ctx context.Context, c *bacnet.Client, deviceID uint32) (Info, error) {
	dev := bacnet.ObjectID{Type: bacnet.Device, Instance: deviceID}
	props := []bacnet.PropertyID{bacnet.PropObjectName, bacnet.PropDescription, bacnet.PropFirmwareRevision,
		bacnet.PropApplicationVersion, bacnet.PropVendorName, bacnet.PropModelName}
	var refs []bacnet.PropertyRef
	for _, p := range props {
		refs = append(refs, bacnet.PropertyRef{Object: dev, Property: p})
	}
	res, err := c.ReadMultiple(ctx, refs)
	if err != nil {
		return Info{}, err
	}
	str := func(p bacnet.PropertyID) string {
		s, _ := res[bacnet.PropertyRef{Object: dev, Property: p}].Value().(string)
		return strings.TrimSpace(s)
	}
	info := Info{
		Addr:            c.Addr,
		DeviceID:        deviceID,
		ObjectName:      str(bacnet.PropObjectName),
		Serial:          str(bacnet.PropDescription),
		Firmware:        str(bacnet.PropFirmwareRevision),
		AppVersion:      str(bacnet.PropApplicationVersion),
		Vendor:          str(bacnet.PropVendorName),
		ControllerModel: str(bacnet.PropModelName),
	}
	info.Model = modelFromSerial(info.Serial)
	return info, nil
}

// Device is a connected unit with resolved point ids.
type Device struct {
	Info   Info
	client *bacnet.Client
	ids    map[string]bacnet.ObjectID // resolved, present points only
	alarms []AlarmDef                 // alarms present on this unit
}

// Resolved returns the resolved point ids, for caching.
func (d *Device) Resolved() map[string]bacnet.ObjectID { return d.ids }

// Has reports whether the unit has the point.
func (d *Device) Has(key string) bool { _, ok := d.ids[key]; return ok }

// Connect reads the unit's identity and resolves point ids. A cache from an
// earlier session is used as-is when the firmware is unchanged.
func Connect(ctx context.Context, addr netip.AddrPort, deviceID uint32, cache map[string]bacnet.ObjectID, cacheFirmware string) (*Device, error) {
	c := bacnet.NewClient(addr)
	info, err := ReadInfo(ctx, c, deviceID)
	if err != nil {
		return nil, err
	}
	d := &Device{Info: info, client: c}
	if len(cache) > 0 && cacheFirmware == info.Firmware+"/"+info.AppVersion {
		d.ids = cache
	} else if d.ids, err = resolve(ctx, c); err != nil {
		return nil, err
	}
	for _, a := range Alarms {
		if d.Has(a.Point().Key) {
			d.alarms = append(d.alarms, a)
		}
	}
	return d, nil
}

// CacheTag identifies the firmware the resolved ids belong to.
func (d *Device) CacheTag() string { return d.Info.Firmware + "/" + d.Info.AppVersion }

func allPoints() []Point {
	out := append([]Point(nil), Points...)
	for _, a := range Alarms {
		out = append(out, a.Point())
	}
	return out
}

// scanLimits bound the instance range searched when a point is not at its
// default id.
var scanLimits = map[bacnet.ObjectType]uint32{
	bacnet.AnalogInput: 200, bacnet.AnalogOutput: 64, bacnet.AnalogValue: 2600,
	bacnet.BinaryOutput: 128, bacnet.BinaryValue: 900, bacnet.MultiStateValue: 900,
	bacnet.PositiveIntegerValue: 600,
}

// resolve checks each point's object name at its default id and searches for
// the ones that moved.
func resolve(ctx context.Context, c *bacnet.Client) (map[string]bacnet.ObjectID, error) {
	points := allPoints()
	var refs []bacnet.PropertyRef
	for _, p := range points {
		prop := bacnet.PropObjectName
		if p.Name == "" {
			prop = bacnet.PropPresentValue
		}
		refs = append(refs, bacnet.PropertyRef{Object: p.Obj, Property: prop})
	}
	res, err := c.ReadMultiple(ctx, refs)
	if err != nil {
		return nil, err
	}
	ids := map[string]bacnet.ObjectID{}
	missing := map[bacnet.ObjectType]map[string]string{} // type -> name -> key
	for i, p := range points {
		r := res[refs[i]]
		if p.Name == "" {
			if r.Err == nil && len(r.Values) > 0 {
				ids[p.Key] = p.Obj
			}
			continue
		}
		if name, _ := r.Value().(string); name == p.Name {
			ids[p.Key] = p.Obj
			continue
		}
		if missing[p.Obj.Type] == nil {
			missing[p.Obj.Type] = map[string]string{}
		}
		missing[p.Obj.Type][p.Name] = p.Key
	}
	for t, want := range missing {
		var refs []bacnet.PropertyRef
		for i := uint32(0); i < scanLimits[t]; i++ {
			refs = append(refs, bacnet.PropertyRef{Object: bacnet.ObjectID{Type: t, Instance: i}, Property: bacnet.PropObjectName})
		}
		res, err := c.ReadMultiple(ctx, refs)
		if err != nil {
			return nil, err
		}
		for ref, r := range res {
			if name, ok := r.Value().(string); ok {
				if key, ok := want[name]; ok {
					ids[key] = ref.Object
				}
			}
		}
	}
	return ids, nil
}

// State is one poll of the unit.
type State struct {
	Time   time.Time
	values map[string]float64

	// Fan output overrides held at the app's command priority (nil when the
	// controller is in charge).
	SupplyOverride  *float64
	ExhaustOverride *float64

	Alarms []ActiveAlarm
}

type ActiveAlarm struct {
	AlarmDef
	State int
}

func (s *State) Has(key string) bool  { _, ok := s.values[key]; return ok }
func (s *State) F(key string) float64 { return s.values[key] }
func (s *State) I(key string) int     { return int(s.values[key] + 0.5) }
func (s *State) B(key string) bool    { return s.values[key] != 0 }

// ExtractTemperature falls back to the alternative sensor used by some models.
func (s *State) ExtractTemperature() float64 {
	if v := s.F(ExtractTemp); v != 0 || !s.Has(ExtractTempAlt) {
		return v
	}
	return s.F(ExtractTempAlt)
}

// FireplaceActive reports whether temporary fireplace ventilation runs.
func (s *State) FireplaceActive() bool {
	return s.I(OperationMode) == OpFireplace || s.B(FireplaceTemp) || s.B(FireplaceState)
}

// RapidActive reports whether temporary rapid ventilation runs.
func (s *State) RapidActive() bool {
	return s.I(OperationMode) == OpRapid || s.B(RapidTemp)
}

// Poll reads the present value of every point.
func (d *Device) Poll(ctx context.Context) (*State, error) {
	var refs []bacnet.PropertyRef
	keys := map[bacnet.ObjectID]string{}
	for key, obj := range d.ids {
		refs = append(refs, bacnet.PropertyRef{Object: obj, Property: bacnet.PropPresentValue})
		keys[obj] = key
	}
	res, err := d.client.ReadMultiple(ctx, refs)
	if err != nil {
		return nil, err
	}
	s := &State{Time: time.Now(), values: map[string]float64{}}
	for ref, r := range res {
		if v, ok := bacnet.AsFloat(r.Value()); ok {
			s.values[keys[ref.Object]] = v
		}
	}
	for _, a := range d.alarms {
		st := s.I(a.Point().Key)
		if st != 0 && st != AlarmNone {
			s.Alarms = append(s.Alarms, ActiveAlarm{AlarmDef: a, State: st})
		}
	}
	s.SupplyOverride, err = d.override(ctx, SupplyFanPct)
	if err != nil {
		return nil, err
	}
	s.ExhaustOverride, err = d.override(ctx, ExhaustFanPct)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *Device) override(ctx context.Context, key string) (*float64, error) {
	obj, ok := d.ids[key]
	if !ok {
		return nil, nil
	}
	vals, err := d.client.ReadProperty(ctx, obj, bacnet.PropPriorityArray)
	if err != nil {
		return nil, err
	}
	if len(vals) >= commandPriority {
		if v, ok := bacnet.AsFloat(vals[commandPriority-1]); ok {
			return &v, nil
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------- actions

func (d *Device) write(ctx context.Context, key string, v float64, priority uint8) error {
	obj, ok := d.ids[key]
	if !ok {
		return fmt.Errorf("this unit has no %s point", key)
	}
	var val bacnet.Value
	switch obj.Type {
	case bacnet.AnalogValue, bacnet.AnalogOutput:
		val = float32(v)
	case bacnet.BinaryValue:
		val = bacnet.Enumerated(uint32(v))
	default:
		if v < 0 {
			return fmt.Errorf("%s must not be negative", key)
		}
		val = uint32(v + 0.5)
	}
	return d.client.WriteProperty(ctx, obj, bacnet.PropPresentValue, val, priority)
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// SetVentilationMode selects Stop/Away/Home/High. The mode only takes
// effect while the comfort (home/away) button is on, so it is switched on
// first for any mode other than Away.
func (d *Device) SetVentilationMode(ctx context.Context, s *State, mode int) error {
	if mode < VentStop || mode > VentHigh {
		return fmt.Errorf("invalid ventilation mode %d", mode)
	}
	if mode != VentAway && s != nil && !s.B(ComfortButton) {
		if err := d.SetComfortButton(ctx, true); err != nil {
			return err
		}
	}
	return d.write(ctx, VentilationMode, float64(mode), commandPriority)
}

// SetComfortButton switches the home/away button. Off means Away after the
// away delay.
func (d *Device) SetComfortButton(ctx context.Context, on bool) error {
	return d.write(ctx, ComfortButton, boolf(on), commandPriority)
}

func (d *Device) SetAwayDelay(ctx context.Context, minutes float64) error {
	if minutes < 0 || minutes > 600 {
		return fmt.Errorf("away delay must be 0-600 minutes")
	}
	return d.write(ctx, AwayDelay, minutes, 0)
}

func (d *Device) SetCookerHood(ctx context.Context, on bool) error {
	return d.write(ctx, CookerHood, boolf(on), commandPriority)
}

func (d *Device) SetFireplaceRuntime(ctx context.Context, minutes float64) error {
	if minutes < 1 || minutes > 360 {
		return fmt.Errorf("runtime must be 1-360 minutes")
	}
	return d.write(ctx, FireplaceRuntime, minutes, 0)
}

// ToggleFireplace starts temporary fireplace ventilation, or stops it when
// it is already running: the trigger toggles.
func (d *Device) ToggleFireplace(ctx context.Context) error {
	return d.write(ctx, FireplaceTrigger, triggerValue, 0)
}

func (d *Device) SetRapidRuntime(ctx context.Context, minutes float64) error {
	if minutes < 1 || minutes > 360 {
		return fmt.Errorf("runtime must be 1-360 minutes")
	}
	return d.write(ctx, RapidRuntime, minutes, 0)
}

// ToggleRapid starts temporary rapid ventilation, or stops it when running.
func (d *Device) ToggleRapid(ctx context.Context) error {
	return d.write(ctx, RapidTrigger, triggerValue, 0)
}

// SetTemperature writes a supply air temperature setpoint (Home or Away).
func (d *Device) SetTemperature(ctx context.Context, key string, celsius float64) error {
	if key != SetpointHome && key != SetpointAway {
		return fmt.Errorf("not a temperature setpoint: %s", key)
	}
	if celsius < 10 || celsius > 30 {
		return fmt.Errorf("setpoint must be 10-30 °C")
	}
	return d.write(ctx, key, celsius, 0)
}

// FanSetpoints lists the per-mode fan setpoints, supply then exhaust.
var FanSetpoints = []struct{ Mode, Supply, Exhaust string }{
	{"Away", SupplySetAway, ExhaustSetAway},
	{"Home", SupplySetHome, ExhaustSetHome},
	{"High", SupplySetHigh, ExhaustSetHigh},
	{"Fireplace", SupplySetFire, ExhaustSetFire},
	{"Cooker hood", SupplySetCooker, ExhaustSetCooker},
}

func (d *Device) SetFanSetpoint(ctx context.Context, key string, percent float64) error {
	ok := false
	for _, f := range FanSetpoints {
		ok = ok || key == f.Supply || key == f.Exhaust
	}
	if !ok {
		return fmt.Errorf("not a fan setpoint: %s", key)
	}
	if percent < 0 || percent > 100 {
		return fmt.Errorf("fan setpoint must be 0-100 %%")
	}
	return d.write(ctx, key, percent, 0)
}

func (d *Device) SetElectricHeater(ctx context.Context, on bool) error {
	return d.write(ctx, HeaterEnabled, boolf(on), 0)
}

func (d *Device) SetFilterInterval(ctx context.Context, hours float64) error {
	if hours < 720 || hours > 8760 {
		return fmt.Errorf("filter interval must be 720-8760 hours (1-12 months)")
	}
	return d.write(ctx, FilterInterval, hours, 0)
}

// ResetFilterTimer restarts the filter operating-time counter after a
// filter change.
func (d *Device) ResetFilterTimer(ctx context.Context) error {
	return d.write(ctx, FilterReset, triggerValue, 0)
}

func (d *Device) AcknowledgeAlarmsB(ctx context.Context) error {
	return d.write(ctx, AlarmAck, alarmAckB, 0)
}

func (d *Device) AcknowledgeAlarmsA(ctx context.Context) error {
	return d.write(ctx, AlarmAck, alarmAckA, 0)
}

func (d *Device) ResetAlarmsA(ctx context.Context) error {
	return d.write(ctx, AlarmAck, alarmResetA, 0)
}

// OverrideFan forces a fan output (SupplyFanPct or ExhaustFanPct) to a fixed
// percentage, bypassing the controller's mode logic. Pass nil to release the
// override and hand control back. The unit never goes below its minimum fan
// speed; use the Stop ventilation mode to stop ventilation.
func (d *Device) OverrideFan(ctx context.Context, key string, percent *float64) error {
	if key != SupplyFanPct && key != ExhaustFanPct {
		return fmt.Errorf("not a fan output: %s", key)
	}
	obj, ok := d.ids[key]
	if !ok {
		return fmt.Errorf("this unit has no %s point", key)
	}
	var val bacnet.Value = bacnet.Null{}
	if percent != nil {
		if *percent < 0 || *percent > 100 {
			return fmt.Errorf("fan output must be 0-100 %%")
		}
		val = float32(*percent)
	}
	return d.client.WriteProperty(ctx, obj, bacnet.PropPresentValue, val, commandPriority)
}
