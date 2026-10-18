package tui

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/oskarkook/flexit-tui/internal/flexit"
)

type rowKind int

const (
	kSection rowKind = iota
	kInfo
	kChoice
	kNumber
	kToggle
	kAction
)

type applyFunc func(ctx context.Context, d *flexit.Device, s *flexit.State, v float64) error

// row is one line of a tab. Interactive rows hold their current value and
// how to apply a new one; the TUI handles stepping, typing and confirmation.
type row struct {
	kind  rowKind
	id    string // stable id for pending edits
	label string
	value string // rendered current value (info rows, and suffix for others)
	hint  string // help shown while selected

	// choice: 0-based index into options
	options []string
	current float64

	// number
	min, max, step float64
	decimals       int
	unit           string

	// action button text
	button string

	apply   applyFunc
	confirm func(v float64) string // non-empty asks before applying
}

func (r row) interactive() bool { return r.kind >= kChoice }

func section(title string) row { return row{kind: kSection, label: title} }

func info(label, value string) row { return row{kind: kInfo, label: label, value: value} }

func (r row) format(v float64) string {
	switch r.kind {
	case kChoice:
		if i := int(v); i >= 0 && i < len(r.options) {
			return r.options[i]
		}
	case kToggle:
		if v != 0 {
			return "On"
		}
		return "Off"
	case kNumber:
		s := fmt.Sprintf("%.*f", r.decimals, v)
		if r.unit == "months" && v == 1 {
			return s + " month"
		}
		if r.unit != "" {
			s += " " + r.unit
		}
		return s
	}
	return ""
}

func (r row) clamp(v float64) float64 {
	v = math.Max(r.min, math.Min(r.max, v))
	if r.step > 0 && r.decimals == 0 {
		v = math.Round(v)
	}
	return v
}

func fmtTemp(s *flexit.State, key string) string {
	if !s.Has(key) {
		return "—"
	}
	return fmt.Sprintf("%.1f °C", s.F(key))
}

func fmtHours(h float64) string {
	if h >= 48 {
		return fmt.Sprintf("%.0f h (%.0f days)", h, h/24)
	}
	return fmt.Sprintf("%.0f h", h)
}

// fmtRemaining renders filter time left as "N days (N months)", or just
// days when under a month.
func fmtRemaining(hours float64) string {
	days := int(hours / 24)
	s := plural(days, "day")
	if hours >= hoursPerMonth {
		s += " (" + plural(int(math.Round(hours/hoursPerMonth)), "month") + ")"
	}
	return s
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// ---------------------------------------------------------------- tabs

type tab struct {
	name string
	rows func(d *flexit.Device, s *flexit.State) []row
}

var tabs = []tab{
	{"Overview", nil}, // custom dashboard
	{"Control", controlRows},
	{"Fans", fanRows},
	{"Filter & alarms", filterAlarmRows},
	{"Info", infoRows},
}

func controlRows(d *flexit.Device, s *flexit.State) []row {
	var rows []row
	rows = append(rows, section("Ventilation"))

	rows = append(rows, row{
		kind: kChoice, id: "vent_mode", label: "Ventilation mode",
		options: flexit.VentilationModes, current: float64(s.I(flexit.VentilationMode) - 1),
		hint: "Selecting Stop, Home or High also switches the Home/Away button on, since the mode only applies while it is on.",
		apply: func(ctx context.Context, d *flexit.Device, s *flexit.State, v float64) error {
			return d.SetVentilationMode(ctx, s, int(v)+1)
		},
		confirm: func(v float64) string {
			if int(v)+1 == flexit.VentStop {
				return "Stop the ventilation unit completely?"
			}
			return ""
		},
	})

	comfort := "Off — Away"
	if s.B(flexit.ComfortButton) {
		comfort = "On — Home"
	}
	if s.B(flexit.AwayDelayActive) {
		comfort += " (away delay running)"
	}
	rows = append(rows, row{
		kind: kToggle, id: "comfort", label: "Home/Away button", current: boolf(s.B(flexit.ComfortButton)),
		value: comfort,
		hint:  "The comfort button. Off switches to Away after the away delay; on follows the ventilation mode.",
		apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
			return d.SetComfortButton(ctx, v != 0)
		},
	})
	if d.Has(flexit.AwayDelay) {
		rows = append(rows, row{
			kind: kNumber, id: "away_delay", label: "Away delay", current: s.F(flexit.AwayDelay),
			min: 0, max: 600, step: 5, unit: "min",
			hint: "Delay before Away takes effect after switching the Home/Away button off.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetAwayDelay(ctx, v)
			},
		})
	}
	if d.Has(flexit.CookerHood) {
		rows = append(rows, row{
			kind: kToggle, id: "cooker", label: "Cooker hood mode", current: boolf(s.B(flexit.CookerHood)),
			hint: "Runs the fans at the cooker hood setpoints until switched off.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetCookerHood(ctx, v != 0)
			},
		})
	}

	rows = append(rows, section("Temporary ventilation"))
	if d.Has(flexit.FireplaceTrigger) {
		r := row{kind: kAction, id: "fireplace", label: "Fireplace ventilation",
			hint: "Raises supply air pressure for lighting a fireplace, for the set duration."}
		if s.FireplaceActive() {
			r.value = stWarn.Render(fmt.Sprintf("running, %.0f min left", s.F(flexit.FireplaceLeft)))
			r.button = "Stop"
		} else {
			r.value = "off"
			r.button = fmt.Sprintf("Start for %.0f min", s.F(flexit.FireplaceRuntime))
		}
		r.apply = func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
			return d.ToggleFireplace(ctx)
		}
		rows = append(rows, r, row{
			kind: kNumber, id: "fireplace_runtime", label: "  Duration", current: s.F(flexit.FireplaceRuntime),
			min: 1, max: 360, step: 5, unit: "min",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetFireplaceRuntime(ctx, v)
			},
		})
	}
	if d.Has(flexit.RapidTrigger) {
		r := row{kind: kAction, id: "rapid", label: "Rapid ventilation",
			hint: "Runs at High for the set duration, then returns to the previous mode."}
		if s.RapidActive() {
			r.value = stWarn.Render(fmt.Sprintf("running, %.0f min left", s.F(flexit.RapidLeft)))
			r.button = "Stop"
		} else {
			r.value = "off"
			r.button = fmt.Sprintf("Start for %.0f min", s.F(flexit.RapidRuntime))
		}
		r.apply = func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
			return d.ToggleRapid(ctx)
		}
		rows = append(rows, r, row{
			kind: kNumber, id: "rapid_runtime", label: "  Duration", current: s.F(flexit.RapidRuntime),
			min: 1, max: 360, step: 5, unit: "min",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetRapidRuntime(ctx, v)
			},
		})
	}

	rows = append(rows, section("Temperature"))
	for _, sp := range []struct{ key, label string }{
		{flexit.SetpointHome, "Setpoint Home"},
		{flexit.SetpointAway, "Setpoint Away"},
	} {
		key := sp.key
		rows = append(rows, row{
			kind: kNumber, id: key, label: sp.label, current: s.F(key),
			min: 10, max: 30, step: 0.5, decimals: 1, unit: "°C",
			hint: "Supply air temperature target. Heat recovery and the electric heater work to reach it.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetTemperature(ctx, key, v)
			},
		})
	}
	if d.Has(flexit.HeaterEnabled) {
		rows = append(rows, row{
			kind: kToggle, id: "heater", label: "Electric heater", current: boolf(s.B(flexit.HeaterEnabled)),
			value: fmt.Sprintf("%s — %.2f of %.2f kW", onOff(s.B(flexit.HeaterEnabled)),
				s.F(flexit.HeaterPower), s.F(flexit.HeaterNominal)),
			hint: "Allows the electric reheater to top up the supply air temperature.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetElectricHeater(ctx, v != 0)
			},
		})
	}
	return rows
}

func fanRows(d *flexit.Device, s *flexit.State) []row {
	minFan := 30.0
	if s.Has(flexit.FanSpeedMin) && s.F(flexit.FanSpeedMin) > 0 {
		minFan = s.F(flexit.FanSpeedMin)
	}
	var rows []row
	rows = append(rows, section(fmt.Sprintf("Fan setpoints per mode (minimum %.0f %%)", minFan)))
	for _, f := range flexit.FanSetpoints {
		for _, side := range []struct{ key, name string }{{f.Supply, "supply"}, {f.Exhaust, "extract"}} {
			if !d.Has(side.key) {
				continue
			}
			key := side.key
			rows = append(rows, row{
				kind: kNumber, id: key, label: f.Mode + " " + side.name, current: s.F(key),
				min: minFan, max: 100, step: 5, unit: "%",
				apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
					return d.SetFanSetpoint(ctx, key, v)
				},
			})
		}
	}

	rows = append(rows, section("Manual fan override (advanced)"))
	for _, f := range []struct {
		key, label, rpm string
		ovr             *float64
	}{
		{flexit.SupplyFanPct, "Supply fan output", flexit.SupplyFanRPM, s.SupplyOverride},
		{flexit.ExhaustFanPct, "Extract fan output", flexit.ExhaustFanRPM, s.ExhaustOverride},
	} {
		if !d.Has(f.key) {
			continue
		}
		key := f.key
		status := "controller"
		if f.ovr != nil {
			status = stWarn.Render(fmt.Sprintf("OVERRIDDEN at %.0f %%", *f.ovr))
		}
		rows = append(rows, row{
			kind: kNumber, id: "ovr_" + key, label: f.label, current: s.F(key),
			min: 0, max: 100, step: 5, unit: "%",
			value: fmt.Sprintf("%.0f rpm — %s", s.F(f.rpm), status),
			hint:  "Forces the fan output directly, bypassing modes, until released. The fan never drops below its minimum speed; use Stop mode to stop.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.OverrideFan(ctx, key, &v)
			},
			confirm: func(v float64) string {
				return fmt.Sprintf("Force the fan output to %.0f %% until released?", v)
			},
		})
		if f.ovr != nil {
			rows = append(rows, row{
				kind: kAction, id: "release_" + key, label: "  Release override", button: "Release",
				hint: "Hands the fan back to the controller. It briefly dips before ramping back.",
				apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
					return d.OverrideFan(ctx, key, nil)
				},
			})
		}
	}
	return rows
}

// hoursPerMonth converts the unit's filter interval (operating hours) to
// months, as the Flexit GO app shows it: 4380 h = 6 months.
const hoursPerMonth = 730

func filterAlarmRows(d *flexit.Device, s *flexit.State) []row {
	var rows []row
	rows = append(rows, section("Air filter"))
	hours, interval := s.F(flexit.FilterHours), s.F(flexit.FilterInterval)
	used := ""
	if interval > 0 {
		used = fmt.Sprintf("  %s", bar(hours/interval, 20))
	}
	rows = append(rows, info("Operating time", fmtHours(hours)+used))
	status := stGood.Render("OK")
	if s.B(flexit.FilterPolluted) {
		status = stBad.Render("Replace filter")
	} else if interval > 0 {
		status += stMuted.Render(" — " + fmtRemaining(math.Max(0, interval-hours)) + " until replacement")
	}
	rows = append(rows, info("Status", status))
	if d.Has(flexit.FilterInterval) {
		rows = append(rows, row{
			kind: kNumber, id: "filter_interval", label: "Replacement interval",
			current: math.Round(interval / hoursPerMonth),
			min:     1, max: 12, step: 1, unit: "months",
			hint: "Time between filter changes. The unit counts operating hours; one month is 730 h.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, v float64) error {
				return d.SetFilterInterval(ctx, v*hoursPerMonth)
			},
		})
	}
	if d.Has(flexit.FilterReset) {
		rows = append(rows, row{
			kind: kAction, id: "filter_reset", label: "Filter changed", button: "Reset filter timer",
			hint: "Do this after replacing the filters. Restarts the operating-time counter.",
			apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
				return d.ResetFilterTimer(ctx)
			},
			confirm: func(float64) string { return "Reset the filter operating time to 0? This cannot be undone." },
		})
	}

	rows = append(rows, section("Alarms"))
	summary := func(key, code string) string {
		st := s.I(key)
		v := flexit.Label(flexit.AlarmSummaryStates, st)
		if st > 1 {
			v = stBad.Render(v)
			if c := s.I(code); c != 0 {
				v += fmt.Sprintf(" (code %d)", c)
			}
		} else {
			v = stGood.Render(v)
		}
		return v
	}
	rows = append(rows,
		info("A alarms (stop unit)", summary(flexit.AlarmStateA, flexit.AlarmCodeA)),
		info("B alarms (maintenance)", summary(flexit.AlarmStateB, flexit.AlarmCodeB)))
	if len(s.Alarms) == 0 {
		rows = append(rows, info("Active alarms", stGood.Render("none")))
	}
	for _, a := range s.Alarms {
		text := a.Text
		if text == "" {
			text = "Alarm"
		}
		st := flexit.Label(flexit.AlarmStates, a.State)
		if a.State == flexit.AlarmActiveUnacked || a.State == flexit.AlarmActiveAcked {
			st = stBad.Render(st)
		} else {
			st = stWarn.Render(st)
		}
		rows = append(rows, info(fmt.Sprintf("%s %d", a.Class, a.Code), text+" — "+st))
	}
	if d.Has(flexit.AlarmAck) {
		rows = append(rows,
			row{kind: kAction, id: "ack_b", label: "Acknowledge B alarms", button: "Acknowledge",
				apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
					return d.AcknowledgeAlarmsB(ctx)
				}},
			row{kind: kAction, id: "ack_a", label: "Acknowledge A alarms", button: "Acknowledge",
				apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
					return d.AcknowledgeAlarmsA(ctx)
				}},
			row{kind: kAction, id: "reset_a", label: "Reset A alarms", button: "Reset",
				hint: "Restarts the unit after an A alarm once its cause is fixed.",
				apply: func(ctx context.Context, d *flexit.Device, _ *flexit.State, _ float64) error {
					return d.ResetAlarmsA(ctx)
				},
				confirm: func(float64) string { return "Reset A alarms and restart the unit?" }},
		)
	}
	return rows
}

func infoRows(d *flexit.Device, s *flexit.State) []row {
	i := d.Info
	rows := []row{
		section("Unit"),
		info("Model", i.Title()),
		info("Serial number", i.Serial),
		info("Address", fmt.Sprintf("%s (BACnet device %d)", i.Addr, i.DeviceID)),
		info("Controller", strings.TrimSpace(i.Vendor+" "+i.ControllerModel)),
		info("Firmware", i.Firmware),
		info("Application", i.AppVersion),

		section("Operating state"),
		info("Operation mode", flexit.Label(flexit.OperationModes, s.I(flexit.OperationMode))),
		info("Plant mode", flexit.Label(flexit.PlantModes, s.I(flexit.PlantMode))),
		info("Heating/cooling", flexit.Label(flexit.HeatCoolStates, s.I(flexit.HeatCoolState))),
		info("Heat exchanger", fmt.Sprintf("%s — %.0f %% speed, %.0f %% efficiency",
			flexit.Label(flexit.RotorModes, s.I(flexit.RotorMode)), s.F(flexit.RotorSpeed), s.F(flexit.RotorEfficiency))),
		info("Electric heater", fmt.Sprintf("%s — %s, %.2f of %.2f kW", onOff(s.B(flexit.HeaterEnabled)),
			flexit.Label(flexit.HeaterModes, s.I(flexit.HeaterMode)), s.F(flexit.HeaterPower), s.F(flexit.HeaterNominal))),
		info("Present setpoint", fmtTemp(s, flexit.SetpointPresent)),
		info("Minimum fan speed", fmt.Sprintf("%.0f %%", s.F(flexit.FanSpeedMin))),

		section("Operating hours"),
	}
	for _, h := range []struct{ key, label string }{
		{flexit.HoursTotal, "Total"},
		{flexit.HoursHome, "Home"},
		{flexit.HoursAway, "Away"},
		{flexit.HoursHigh, "High"},
		{flexit.HoursStop, "Stop"},
		{flexit.HoursFireplace, "Fireplace"},
		{flexit.HoursCookerHood, "Cooker hood"},
		{flexit.HoursRotor, "Heat exchanger"},
		{flexit.HoursHeater, "Electric heater"},
	} {
		if s.Has(h.key) {
			rows = append(rows, info(h.label, fmtHours(s.F(h.key))))
		}
	}
	return rows
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func bar(frac float64, width int) string {
	frac = math.Max(0, math.Min(1, frac))
	n := int(math.Round(frac * float64(width)))
	st := stGood
	switch {
	case frac >= 1:
		st = stBad
	case frac >= 0.85:
		st = stWarn
	}
	return st.Render(strings.Repeat("█", n)) + stMuted.Render(strings.Repeat("░", width-n))
}
