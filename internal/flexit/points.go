// Package flexit maps a Flexit Nordic unit's BACnet objects to typed state
// and actions.
package flexit

import (
	"fmt"

	"github.com/oskarkook/flexit-tui/internal/bacnet"
)

// Point is one BACnet object the app reads or writes. Object ids differ
// between firmware versions (e.g. the filter-timer reset is multiStateValue
// 613 on some units and 609 on others), but the Siemens object names are
// stable, so every point carries its name and is re-resolved when the
// object at the default id has a different name.
type Point struct {
	Key  string
	Obj  bacnet.ObjectID
	Name string
}

func pt(key string, t bacnet.ObjectType, id uint32, name string) Point {
	return Point{Key: key, Obj: bacnet.ObjectID{Type: t, Instance: id}, Name: name}
}

const (
	ai  = bacnet.AnalogInput
	ao  = bacnet.AnalogOutput
	av  = bacnet.AnalogValue
	bo  = bacnet.BinaryOutput
	bv  = bacnet.BinaryValue
	msv = bacnet.MultiStateValue
	piv = bacnet.PositiveIntegerValue
)

// Point keys.
const (
	OutsideTemp      = "outside_temp"
	SupplyTemp       = "supply_temp"
	ExhaustTemp      = "exhaust_temp"
	ExtractTemp      = "extract_temp"
	ExtractTempAlt   = "extract_temp_alt"
	RoomTemp         = "room_temp"
	ExtractHumidity  = "extract_humidity"
	Room1Humidity    = "room1_humidity"
	Room2Humidity    = "room2_humidity"
	Room3Humidity    = "room3_humidity"
	AirQuality       = "air_quality"
	SupplyFanRPM     = "supply_fan_rpm"
	ExhaustFanRPM    = "exhaust_fan_rpm"
	SupplyFanPct     = "supply_fan_pct"
	ExhaustFanPct    = "exhaust_fan_pct"
	FanSpeedMin      = "fan_speed_min"
	RotorSpeed       = "rotor_speed"
	RotorEfficiency  = "rotor_efficiency"
	RotorMode        = "rotor_mode"
	HeaterEnabled    = "heater_enabled"
	HeaterMode       = "heater_mode"
	HeaterNominal    = "heater_nominal"
	HeaterPower      = "heater_power"
	HeatCoolState    = "heat_cool_state"
	PlantMode        = "plant_mode"
	OperationMode    = "operation_mode"
	VentilationMode  = "ventilation_mode"
	ComfortButton    = "comfort_button"
	AwayDelay        = "away_delay"
	AwayDelayActive  = "away_delay_active"
	CookerHood       = "cooker_hood"
	FireplaceTrigger = "fireplace_trigger"
	FireplaceState   = "fireplace_state"
	FireplaceTemp    = "fireplace_temporary"
	FireplaceRuntime = "fireplace_runtime"
	FireplaceLeft    = "fireplace_remaining"
	RapidTrigger     = "rapid_trigger"
	RapidTemp        = "rapid_temporary"
	RapidRuntime     = "rapid_runtime"
	RapidLeft        = "rapid_remaining"
	SetpointHome     = "setpoint_home"
	SetpointAway     = "setpoint_away"
	SetpointPresent  = "setpoint_present"
	SupplySetHigh    = "supply_set_high"
	SupplySetHome    = "supply_set_home"
	SupplySetAway    = "supply_set_away"
	SupplySetFire    = "supply_set_fire"
	SupplySetCooker  = "supply_set_cooker"
	ExhaustSetHigh   = "exhaust_set_high"
	ExhaustSetHome   = "exhaust_set_home"
	ExhaustSetAway   = "exhaust_set_away"
	ExhaustSetFire   = "exhaust_set_fire"
	ExhaustSetCooker = "exhaust_set_cooker"
	FilterHours      = "filter_hours"
	FilterInterval   = "filter_interval"
	FilterPolluted   = "filter_polluted"
	FilterReset      = "filter_reset"
	AlarmStateA      = "alarm_state_a"
	AlarmStateB      = "alarm_state_b"
	AlarmCodeA       = "alarm_code_a"
	AlarmCodeB       = "alarm_code_b"
	AlarmOutput      = "alarm_output"
	AlarmAck         = "alarm_ack"
	HoursTotal       = "hours_total"
	HoursRotor       = "hours_rotor"
	HoursHeater      = "hours_heater"
	HoursStop        = "hours_stop"
	HoursAway        = "hours_away"
	HoursHome        = "hours_home"
	HoursHigh        = "hours_high"
	HoursFireplace   = "hours_fireplace"
	HoursCookerHood  = "hours_cooker_hood"
)

// Points lists every non-alarm point. Default ids and names were read from a
// CL3 REL (firmware FW=03.39.03.38, application 2.431).
var Points = []Point{
	pt(OutsideTemp, ai, 1, "R(1)'TOa"),
	pt(SupplyTemp, ai, 4, "R(1)'TSu"),
	pt(SupplyFanRPM, ai, 5, "R(1)'FanSuSpdFb"),
	pt(ExhaustTemp, ai, 11, "R(1)'TEh"),
	pt(ExhaustFanRPM, ai, 12, "R(1)'FanEhSpdFb"),
	pt(ExtractTemp, ai, 59, "R(1)'TEx"),
	pt(RoomTemp, ai, 75, "R(1)'ROpUnDev'TR"),
	pt(Room1Humidity, ai, 84, "R(1)'ROpUnDevVmsh1'HuRVmsh1"),
	pt(Room2Humidity, ai, 87, "R(1)'ROpUnDevVmsh2'HuRVmsh2"),
	pt(Room3Humidity, ai, 90, "R(1)'ROpUnDevVmsh3'HuRVmsh3"),
	// Present on some models only (per flexit_bacnet); names unknown, so
	// these are used at their default ids when they exist.
	pt(ExtractTempAlt, ai, 95, ""),
	pt(ExtractHumidity, ai, 96, ""),

	pt(RotorSpeed, ao, 0, "R(1)'RotHExgSpd"),
	pt(SupplyFanPct, ao, 3, "R(1)'FanSuSpd"),
	pt(ExhaustFanPct, ao, 4, "R(1)'FanEhSpd"),

	pt(HeaterNominal, av, 190, "R(1)'HVAC'Hcl'NomElPwr"),
	pt(HeaterPower, av, 194, "R(1)'HVAC'Hcl'HclElPwr"),
	pt(FilterHours, av, 285, "R(1)'HVAC'AlmBdl'TiOpFil"),
	pt(FilterInterval, av, 286, "R(1)'HVAC'AlmBdl'TiOpFilRpc"),
	pt(AlarmCodeA, av, 1794, "R(1)'RHvacCoo'AlmFnct'PrAalmCode"),
	pt(HoursFireplace, av, 1814, "R(1)'RHvacCoo'FplcVntOp'OphFplcVnt"),
	pt(HoursCookerHood, av, 1820, "R(1)'RHvacCoo'FhVntOp'OphFhVnt"),
	pt(SupplySetHigh, av, 1835, "R(1)'RHvacCoo'VntCtl'SpFanSuSpdHi"),
	pt(SupplySetHome, av, 1836, "R(1)'RHvacCoo'VntCtl'SpFanSuSpdHome"),
	pt(SupplySetAway, av, 1837, "R(1)'RHvacCoo'VntCtl'SpFanSuSpdAway"),
	pt(SupplySetFire, av, 1838, "R(1)'RHvacCoo'VntCtl'SpFanSuSpdFplc"),
	pt(SupplySetCooker, av, 1839, "R(1)'RHvacCoo'VntCtl'SpFanSuSpdCkr"),
	pt(ExhaustSetHigh, av, 1840, "R(1)'RHvacCoo'VntCtl'SpFanEhSpdHi"),
	pt(ExhaustSetHome, av, 1841, "R(1)'RHvacCoo'VntCtl'SpFanEhSpdHome"),
	pt(ExhaustSetAway, av, 1842, "R(1)'RHvacCoo'VntCtl'SpFanEhSpdAway"),
	pt(ExhaustSetFire, av, 1843, "R(1)'RHvacCoo'VntCtl'SpFanEhSpdFplc"),
	pt(ExhaustSetCooker, av, 1844, "R(1)'RHvacCoo'VntCtl'SpFanEhSpdCkr"),
	pt(AlarmCodeB, av, 1846, "R(1)'RHvacCoo'MntnFnct'PrBalmCode"),
	pt(HoursTotal, av, 1847, "R(1)'HVAC'AlmBdl'OphDev"),
	pt(HoursRotor, av, 1851, "R(1)'HVAC'Erc'OphErc"),
	pt(HoursHeater, av, 1879, "R(1)'HVAC'Hcl'OphHcl"),
	pt(HoursStop, av, 1913, "R(1)'ROpModDtr'OphStop"),
	pt(HoursAway, av, 1914, "R(1)'ROpModDtr'OphAway"),
	pt(HoursHome, av, 1915, "R(1)'ROpModDtr'OphHome"),
	pt(HoursHigh, av, 1916, "R(1)'ROpModDtr'OphHi"),
	pt(SetpointPresent, av, 1920, "R(1)'RHvacCoo'TCtlH'SpTHrv"),
	pt(SetpointAway, av, 1985, "R(1)'RHvacCoo'TCtlH'SpTAway"),
	pt(SetpointHome, av, 1994, "R(1)'RHvacCoo'TCtlH'SpTHome"),
	pt(RotorEfficiency, av, 2023, "R(1)'HVAC'Erc'PrHExgEfcy"),
	pt(RapidLeft, av, 2031, "R(1)'RHvacCoo'RpdVntOp'TiRmgRpdVnt"),
	pt(FireplaceLeft, av, 2038, "R(1)'RHvacCoo'FplcVntOp'TiRmgFplcVnt"),
	pt(AirQuality, av, 2042, "R(1)'Modbus'AQualR(2)"),
	pt(FanSpeedMin, av, 2091, "R(1)'HVAC'FanSu'FanSpdMinRel"),

	pt(AlarmOutput, bo, 20, "R(1)'CmnAlmIndOut"),

	pt(ComfortButton, bv, 50, "R(1)'ROpModDtr'CmfBtn"),
	pt(FireplaceState, bv, 400, "R(1)'RHvacCoo'FplcVntOp'FplcVnt"),
	pt(CookerHood, bv, 402, "R(1)'RHvacCoo'FhVntOp'FhVnt"),
	pt(HeaterEnabled, bv, 445, "R(1)'HVAC'Hcl'EnHclEl"),
	pt(FireplaceTemp, bv, 453, "R(1)'ROpUnDev'TmpFplcVnt"),
	pt(RapidTemp, bv, 454, "R(1)'ROpUnDev'TmpRpdVnt"),
	pt(FilterPolluted, bv, 522, "R(1)'HVAC'AlmBdl'Alm1020"),
	pt(AwayDelayActive, bv, 574, "R(1)'ROpModDtr'DlyAwayAct"),

	pt(AlarmStateA, msv, 7, "R(1)'RHvacCoo'AlmFnct'AalmSta"),
	pt(HeatCoolState, msv, 13, "R(1)'RHvacCoo'HCStaDtr'HCSta"),
	pt(PlantMode, msv, 14, "R(1)'RHvacCoo'PltModDtr'PltOpMod"),
	pt(AlarmStateB, msv, 18, "R(1)'RHvacCoo'MntnFnct'BalmSta"),
	pt(RotorMode, msv, 21, "R(1)'HVAC'Erc'RotHExgDevMod"),
	pt(HeaterMode, msv, 26, "R(1)'HVAC'Hcl'HclDevMod"),
	pt(VentilationMode, msv, 42, "R(1)'ROpModDtr'ROpMod"),
	pt(RapidTrigger, msv, 357, "R(1)'RHvacCoo'RpdVntOp'TmpRpdVntTrg"),
	pt(FireplaceTrigger, msv, 360, "R(1)'RHvacCoo'FplcVntOp'TmpFplcVntTrg"),
	pt(OperationMode, msv, 361, "R(1)'ROpModDtr'HrvSta"),
	pt(AlarmAck, msv, 434, "R(1)'AlmHdl'AlmAck"),
	pt(FilterReset, msv, 609, "R(1)'HVAC'AlmBdl'FilRpcRst"),

	pt(FireplaceRuntime, piv, 270, "R(1)'RHvacCoo'FplcVntOp'TiRnFplcVnt"),
	pt(RapidRuntime, piv, 293, "R(1)'RHvacCoo'RpdVntOp'TiRnRpdVnt"),
	pt(AwayDelay, piv, 318, "R(1)'ROpModDtr'DlyOnAway"),
}

// AlarmDef describes one entry of the unit's alarm handler. Its state object
// is multiStateValue "R(1)'AlmHdl'Alm<code>'AlmSta".
type AlarmDef struct {
	Code  int
	Text  string
	Class string // "A" (stops the unit) or "B" (maintenance), per AlmType
	Obj   uint32 // default multiStateValue id of the AlmSta object
}

func (a AlarmDef) Point() Point {
	return pt(fmt.Sprintf("alarm_%d", a.Code), msv, a.Obj,
		fmt.Sprintf("R(1)'AlmHdl'Alm%d'AlmSta", a.Code))
}

// Alarms lists the alarm handler entries. Texts come from the corresponding
// binaryValue descriptions; entries the unit leaves undescribed show their
// code only.
var Alarms = []AlarmDef{
	{1001, "Supply air temperature sensor fault", "A", 435},
	{1002, "Exhaust air temperature sensor fault", "B", 437},
	{1003, "Extract air temperature sensor fault", "B", 439},
	{1004, "Outside air temperature sensor fault", "B", 441},
	{1005, "Heating coil frost-protection sensor fault", "A", 443},
	{1006, "Extract air humidity sensor fault", "B", 445},
	{1007, "Rotary heat exchanger motor stuck", "B", 447},
	{1008, "Rotary heat exchanger belt broken", "B", 449},
	{1009, "Fire damper position feedback fault", "A", 451},
	{1010, "Supply air fan speed feedback fault", "B", 453},
	{1011, "Exhaust air fan speed feedback fault", "B", 455},
	{1020, "Air filter polluted", "B", 457},
	{1022, "", "B", 563},
	{1023, "", "B", 565},
	{1024, "", "B", 567},
	{1025, "", "B", 569},
	{1026, "", "B", 571},
	{1027, "", "B", 573},
	{1028, "", "B", 575},
	{1029, "Reheat zone frost-protection sensor fault", "A", 577},
	{1030, "Zone supply air temperature sensor fault", "B", 579},
	{1032, "Supply air pressure sensor fault", "B", 459},
	{1033, "Extract air pressure sensor fault", "B", 461},
	{1034, "Supply fan differential pressure sensor fault", "B", 463},
	{1035, "Exhaust fan differential pressure sensor fault", "B", 465},
	{1036, "", "A", 581},
	{1039, "Rotary heat exchanger motor short circuit", "B", 611},
	{1040, "RF device battery low", "B", 467},
	{2001, "Emergency off activated", "A", 469},
	{2002, "Smoke detector tripped", "A", 471},
	{2003, "CO detector tripped", "A", 473},
	{2004, "Fire alarm activated", "A", 475},
	{2005, "Supply air temperature outside operating limits", "B", 477},
	{2007, "Heating coil frost warning", "A", 479},
	{2008, "", "A", 495},
	{2009, "Reheating coil zone overtemperature", "A", 497},
	{2010, "Heating coil overtemperature", "A", 481},
	{2011, "Reheating coil zone frost warning", "A", 499},
	{2013, "Outside air damper stops air flow", "A", 605},
	{2014, "Heat pump air damper stops air flow", "A", 501},
	{2016, "", "B", 505},
	{2018, "", "B", 607},
	{2019, "", "B", 622},
	{2020, "", "B", 625},
	{2024, "Heating coil lost control", "B", 629},
	{2025, "Heat exchanger lost control", "B", 631},
	{3001, "Heat pump controller Modbus communication error", "B", 483},
	{3002, "I/O extension module 1 Modbus communication error", "A", 485},
	{3003, "I/O extension module 2 Modbus communication error", "B", 487},
	{3004, "Differential pressure sensor Modbus communication error", "B", 489},
	{3006, "RF interface device Modbus communication error", "B", 491},
	{3007, "RF communication error", "B", 493},
}

func init() {
	// 9001-9028 are undocumented; their A/B classes come from the unit's
	// AlmType objects.
	classes := "AABBBAABBBBBABBBBBBBBBBAAABA"
	for i, c := range classes {
		Alarms = append(Alarms, AlarmDef{Code: 9001 + i, Class: string(c), Obj: uint32(507 + 2*i)})
	}
}

// Enumerations, as reported by the unit's State_Text properties
// (multi-state values are 1-based).

var VentilationModes = []string{"Stop", "Away", "Home", "High"}

const (
	VentStop = 1
	VentAway = 2
	VentHome = 3
	VentHigh = 4
)

var OperationModes = []string{"Off", "Away", "Home", "High", "Cooker hood", "Fireplace", "Rapid ventilation"}

const (
	OpFireplace = 6
	OpRapid     = 7
)

var PlantModes = []string{"Off", "Protection", "Economy", "Pre-comfort", "Comfort", "Warm-up", "Cool down",
	"Room low temp. protection", "Condensation overflow protection", "Free cooling", "Night cooling",
	"Ventilation", "Equipment temp. protection", "Air volume flow off", "Smoke extraction (pos. pressure)",
	"Smoke extraction (neg. pressure)", "Purge"}

var HeatCoolStates = []string{"Neither", "Heating", "Cooling"}

var RotorModes = []string{"Off", "Control mode", "Maximum speed", "Minimum speed", "Bypass only"}

var HeaterModes = []string{"Off", "Control mode", "Fully open"}

var AlarmSummaryStates = []string{"OK", "Unacknowledged", "Locked"}

var AlarmStates = []string{"No alarm", "Active, unacknowledged", "Active, acknowledged",
	"Cleared, acknowledged", "Cleared, unacknowledged"}

const (
	AlarmNone           = 1
	AlarmActiveUnacked  = 2
	AlarmActiveAcked    = 3
	AlarmClearedAcked   = 4
	AlarmClearedUnacked = 5
	alarmAckB           = 2
	alarmAckA           = 3
	alarmResetA         = 4
	triggerValue        = 2
	commandPriority     = 13
	controllerPriority  = 15
)

// Label returns the 1-based state text, or the raw number.
func Label(texts []string, v int) string {
	if v >= 1 && v <= len(texts) {
		return texts[v-1]
	}
	return fmt.Sprintf("#%d", v)
}
