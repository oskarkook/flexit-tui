package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/oskarkook/flexit-tui/internal/flexit"
)

const labelWidth = 28

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "Flexit"
	if m.dev != nil {
		v.WindowTitle = "Flexit — " + m.dev.Info.Title()
	}
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	header := m.renderHeader()
	tabsLine := m.renderTabs()
	footer := m.renderFooter()

	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(tabsLine) - lipgloss.Height(footer) - 1
	var body string
	switch m.modal {
	case modalDevices:
		body = m.place(bodyHeight, m.renderDeviceMenu())
	case modalConfirm:
		body = m.place(bodyHeight, stModal.Render(stBold.Render(m.confirmMsg)+"\n\n"+
			stKey.Render("y")+" confirm   "+stKey.Render("n")+" cancel"))
	case modalInput:
		body = m.place(bodyHeight, stModal.Render(stBold.Render(m.inputLabel)+"\n\n"+m.input.View()+"\n\n"+
			stKey.Render("enter")+" apply   "+stKey.Render("esc")+" cancel"))
	default:
		body = m.renderBody(bodyHeight)
	}
	body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, tabsLine, body, footer)
}

func (m Model) place(h int, s string) string {
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, s)
}

func (m Model) renderHeader() string {
	title := stTitle.Render("Flexit")
	if d := m.cfg.Find(m.current); d != nil {
		name := d.Title
		if name == "" {
			name = d.Addr
		}
		addr := d.Addr
		if m.dev != nil {
			addr = m.dev.Info.Addr.String()
		}
		title += "  " + stBold.Render(name) + stMuted.Render("  "+addr)
	}
	if len(m.cfg.Devices) > 1 {
		title += "   " + stKey.Render(fmt.Sprintf("▾ %d units", len(m.cfg.Devices))) + stMuted.Render(" (u)")
	}

	var status string
	switch {
	case m.discovering:
		status = m.spin.View() + " scanning network"
	case m.busy != "":
		status = m.spin.View() + " " + m.busy
	case m.connecting:
		status = m.spin.View() + " connecting"
	case m.connErr != nil:
		status = stBad.Render("unreachable")
	case m.pollErr != nil:
		status = stBad.Render("update failed")
	case m.state != nil:
		ago := time.Since(m.state.Time).Round(time.Second)
		status = stMuted.Render(fmt.Sprintf("updated %s ago", ago))
		if m.polling {
			status = m.spin.View() + " " + status
		}
	}
	gap := max(1, m.width-lipgloss.Width(title)-lipgloss.Width(status))
	return title + strings.Repeat(" ", gap) + status
}

func (m Model) renderTabs() string {
	var parts []string
	for i, t := range tabs {
		label := fmt.Sprintf("%d %s", i+1, t.name)
		if i == m.tab {
			parts = append(parts, stTabActive.Render(label))
		} else {
			parts = append(parts, stTab.Render(label))
		}
	}
	line := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return line + "\n" + stMuted.Render(strings.Repeat("─", m.width))
}

func (m Model) renderFooter() string {
	var lines []string
	if r, ok := m.selectedRow(); ok && m.tab != 0 && m.modal == modalNone && r.hint != "" {
		lines = append(lines, stMuted.Render(r.hint))
	}
	if m.flash != "" && time.Since(m.flashTime) < 8*time.Second {
		if m.flashErr {
			lines = append(lines, stBad.Render(m.flash))
		} else {
			lines = append(lines, stGood.Render(m.flash))
		}
	}
	if m.pollErr != nil && m.state != nil {
		lines = append(lines, stWarn.Render("Showing last known values; "+m.pollErr.Error()))
	}
	keys := []string{"1-5/tab views"}
	if m.tab != 0 {
		keys = append(keys, "↑↓ select", "←→ adjust", "enter apply", "esc cancel")
	}
	if m.tab != 0 {
		keys = append(keys, "e type value")
	}
	keys = append(keys, "u units", "s scan", "a add by IP", "q quit")
	help := ""
	for i, k := range keys {
		if i > 0 {
			help += stMuted.Render(" · ")
		}
		parts := strings.SplitN(k, " ", 2)
		help += stKey.Render(parts[0]) + " " + stMuted.Render(parts[1])
	}
	lines = append(lines, help)
	return strings.Join(lines, "\n")
}

func (m Model) renderBody(height int) string {
	if m.current == "" {
		msg := "No units known yet.\n\nPress " + stKey.Render("s") + " to scan the local network, or " +
			stKey.Render("a") + " to add a unit by IP address."
		if m.discovering {
			msg = m.spin.View() + " Scanning the network for Flexit units…"
		} else if m.discovered {
			msg = "No Flexit units found on the network.\n\nPress " + stKey.Render("s") + " to scan again, or " +
				stKey.Render("a") + " to add a unit by IP address\n(find it in the Flexit GO app: More → Installer → Communication → BACnet)."
		}
		return m.place(height, msg)
	}
	if m.dev == nil {
		d := m.cfg.Find(m.current)
		addr := ""
		if d != nil {
			addr = d.Addr
		}
		if m.connErr != nil {
			return m.place(height, stBad.Render("Cannot reach the unit at "+addr)+"\n\n"+
				stMuted.Render(m.connErr.Error())+"\n\nRetrying every 5 s. Press "+stKey.Render("s")+
				" to scan the network in case its address changed, or "+stKey.Render("u")+" to pick another unit.")
		}
		return m.place(height, m.spin.View()+" Connecting to "+addr+"…")
	}
	if m.state == nil {
		if m.pollErr != nil {
			return m.place(height, stBad.Render("Reading the unit failed: ")+m.pollErr.Error()+"\n\nRetrying…")
		}
		return m.place(height, m.spin.View()+" Reading the unit…")
	}
	if m.tab == 0 {
		return m.renderOverview()
	}
	return m.renderRows(height)
}

// ---------------------------------------------------------------- rows

func (m Model) renderRows(height int) string {
	rows := m.rows()
	cursor := m.cursor[m.tab]
	var lines []string
	cursorLine := 0
	for i, r := range rows {
		if r.kind == kSection {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, stSection.UnsetMarginTop().Render(r.label))
			continue
		}
		if i == cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, m.renderRow(r, i == cursor))
	}
	// Scroll to keep the cursor visible.
	if len(lines) > height && height > 0 {
		start := max(0, min(cursorLine-height/2, len(lines)-height))
		lines = lines[start : start+height]
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderRow(r row, selected bool) string {
	label := fmt.Sprintf("%-*s", labelWidth, r.label)
	prefix := "  "
	if selected {
		prefix = stKey.Render("› ")
		label = stSelected.Render(label)
	} else if r.kind == kInfo {
		label = stMuted.Render(label)
	}

	pending, hasPending := m.pending[r.id]
	var val string
	switch r.kind {
	case kInfo:
		val = r.value
	case kChoice:
		var opts []string
		for i, o := range r.options {
			switch {
			case hasPending && float64(i) == pending:
				opts = append(opts, stPending.Render("‹"+o+"›"))
			case float64(i) == r.current:
				opts = append(opts, stBold.Render("["+o+"]"))
			default:
				opts = append(opts, stMuted.Render(" "+o+" "))
			}
		}
		val = strings.Join(opts, " ")
	case kNumber:
		val = stBold.Render(r.format(r.current))
		if hasPending {
			val += stPending.Render(" → " + r.format(pending))
		}
		if r.value != "" {
			val += stMuted.Render("   " + r.value)
		}
	case kToggle:
		on := r.current != 0
		val = stMuted.Render("○ Off")
		if on {
			val = stGood.Render("● On")
		}
		if r.value != "" {
			val = r.value
			if on {
				val = stGood.Render(val)
			}
		}
		if hasPending {
			val += stPending.Render(" → " + r.format(pending))
		}
	case kAction:
		if r.value != "" {
			val = r.value + "  "
		}
		btn := "[ " + r.button + " ]"
		if selected {
			btn = stKey.Render(btn)
		} else {
			btn = stMuted.Render(btn)
		}
		val += btn
	}
	if hasPending && selected {
		val += stMuted.Render("   enter apply · esc cancel")
	}
	return prefix + label + " " + val
}

// ---------------------------------------------------------------- overview

func (m Model) renderOverview() string {
	s := m.state
	w := min(64, max(30, (m.width-4)/2))

	// Mode banner
	mode := flexit.Label(flexit.OperationModes, s.I(flexit.OperationMode))
	banner := stBig.Render(strings.ToUpper(mode))
	var extra []string
	if s.FireplaceActive() {
		extra = append(extra, stWarn.Render(fmt.Sprintf("fireplace %.0f min left", s.F(flexit.FireplaceLeft))))
	}
	if s.RapidActive() {
		extra = append(extra, stWarn.Render(fmt.Sprintf("rapid ventilation %.0f min left", s.F(flexit.RapidLeft))))
	}
	if s.B(flexit.CookerHood) {
		extra = append(extra, stWarn.Render("cooker hood on"))
	}
	if !s.B(flexit.ComfortButton) {
		extra = append(extra, "home/away button off")
	}
	if s.B(flexit.AwayDelayActive) {
		extra = append(extra, "away delay running")
	}
	if s.SupplyOverride != nil || s.ExhaustOverride != nil {
		extra = append(extra, stWarn.Render("fan override active"))
	}
	if len(s.Alarms) > 0 || s.I(flexit.AlarmStateA) > 1 || s.I(flexit.AlarmStateB) > 1 {
		extra = append(extra, stBad.Render(fmt.Sprintf("%d alarm(s)", len(s.Alarms))))
	}
	if s.B(flexit.FilterPolluted) {
		extra = append(extra, stBad.Render("replace filter"))
	}
	bannerLine := banner + "  " + stMuted.Render("ventilation mode: ") +
		stBold.Render(flexit.Label(flexit.VentilationModes, s.I(flexit.VentilationMode)))
	if len(extra) > 0 {
		bannerLine += "\n" + strings.Join(extra, stMuted.Render(" · "))
	}

	kv := func(k, v string) string { return fmt.Sprintf("%s %s", stMuted.Render(fmt.Sprintf("%-18s", k)), v) }

	temps := []string{stTitle.Render("Temperatures"),
		kv("Outside", fmtTemp(s, flexit.OutsideTemp)),
		kv("Supply", fmtTemp(s, flexit.SupplyTemp)+stMuted.Render(fmt.Sprintf("  target %.1f °C", s.F(flexit.SetpointPresent)))),
		kv("Extract", fmt.Sprintf("%.1f °C", s.ExtractTemperature())),
		kv("Exhaust", fmtTemp(s, flexit.ExhaustTemp)),
	}
	if s.F(flexit.RoomTemp) != 0 {
		temps = append(temps, kv("Room (panel)", fmtTemp(s, flexit.RoomTemp)))
	}
	for _, h := range []struct{ key, label string }{
		{flexit.ExtractHumidity, "Extract humidity"},
		{flexit.Room1Humidity, "Room 1 humidity"},
		{flexit.Room2Humidity, "Room 2 humidity"},
		{flexit.Room3Humidity, "Room 3 humidity"},
	} {
		if s.F(h.key) != 0 {
			temps = append(temps, kv(h.label, fmt.Sprintf("%.0f %%", s.F(h.key))))
		}
	}
	if s.F(flexit.AirQuality) != 0 {
		temps = append(temps, kv("Air quality", fmt.Sprintf("%.0f ppm", s.F(flexit.AirQuality))))
	}

	fanLine := func(pct, rpm string, ovr *float64) string {
		l := fmt.Sprintf("%3.0f %% %s %4.0f rpm", s.F(pct), bar(s.F(pct)/100, 10), s.F(rpm))
		if ovr != nil {
			l += stWarn.Render(" override")
		}
		return l
	}
	fans := []string{stTitle.Render("Fans"),
		kv("Supply", fanLine(flexit.SupplyFanPct, flexit.SupplyFanRPM, s.SupplyOverride)),
		kv("Extract", fanLine(flexit.ExhaustFanPct, flexit.ExhaustFanRPM, s.ExhaustOverride)),
		"",
		stTitle.Render("Heat recovery"),
		kv("Rotor speed", fmt.Sprintf("%.0f %%", s.F(flexit.RotorSpeed))),
		kv("Efficiency", fmt.Sprintf("%.0f %%", s.F(flexit.RotorEfficiency))),
		kv("Electric heater", fmt.Sprintf("%s  %.2f kW", onOff(s.B(flexit.HeaterEnabled)), s.F(flexit.HeaterPower))),
	}

	hours, interval := s.F(flexit.FilterHours), s.F(flexit.FilterInterval)
	filter := stGood.Render("OK")
	if s.B(flexit.FilterPolluted) {
		filter = stBad.Render("replace")
	}
	frac := 0.0
	if interval > 0 {
		frac = hours / interval
	}
	status := []string{stTitle.Render("Filter & alarms"),
		kv("Filter", fmt.Sprintf("%s  %s  %.0f / %.0f h", filter, bar(frac, 10), hours, interval)),
	}
	if len(s.Alarms) == 0 {
		status = append(status, kv("Alarms", stGood.Render("none")))
	}
	for _, a := range s.Alarms {
		text := a.Text
		if text == "" {
			text = "alarm"
		}
		status = append(status, kv(fmt.Sprintf("Alarm %s%d", a.Class, a.Code), stBad.Render(text)))
	}

	setpoints := []string{stTitle.Render("Setpoints"),
		kv("Home", fmtTemp(s, flexit.SetpointHome)+stMuted.Render(fmt.Sprintf("  fans %.0f/%.0f %%", s.F(flexit.SupplySetHome), s.F(flexit.ExhaustSetHome)))),
		kv("Away", fmtTemp(s, flexit.SetpointAway)+stMuted.Render(fmt.Sprintf("  fans %.0f/%.0f %%", s.F(flexit.SupplySetAway), s.F(flexit.ExhaustSetAway)))),
		kv("High", stMuted.Render(fmt.Sprintf("fans %.0f/%.0f %%", s.F(flexit.SupplySetHigh), s.F(flexit.ExhaustSetHigh)))),
	}

	panel := func(lines []string) string { return stPanel.Width(w).Render(strings.Join(lines, "\n")) }
	var grid string
	if m.width >= 2*48 {
		left := lipgloss.JoinVertical(lipgloss.Left, panel(temps), panel(setpoints))
		right := lipgloss.JoinVertical(lipgloss.Left, panel(fans), panel(status))
		grid = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		w = min(64, max(30, m.width-2))
		grid = lipgloss.JoinVertical(lipgloss.Left, panel(temps), panel(fans), panel(status), panel(setpoints))
	}
	return bannerLine + "\n\n" + grid
}

// ---------------------------------------------------------------- device menu

func (m Model) renderDeviceMenu() string {
	var b strings.Builder
	b.WriteString(stBold.Render("Choose a unit") + "\n\n")
	if len(m.cfg.Devices) == 0 {
		b.WriteString(stMuted.Render("No units known yet.") + "\n")
	}
	titleW, addrW := 0, 0
	for _, d := range m.cfg.Devices {
		titleW = max(titleW, len(deviceTitle(d.Title)))
		addrW = max(addrW, len(d.Addr))
	}
	for i, d := range m.cfg.Devices {
		line := fmt.Sprintf("%-*s  %-*s  %s", titleW, deviceTitle(d.Title), addrW, d.Addr, d.Serial)
		mark := "  "
		if d.Key == m.current {
			mark = stGood.Render("● ")
		}
		state := ""
		if m.discovered && !m.online[d.Key] {
			state = stMuted.Render("  not found in last scan")
		}
		if i == m.menuCursor {
			line = stSelected.Render(line)
		}
		b.WriteString(mark + line + state + "\n")
	}
	b.WriteString("\n" + stKey.Render("enter") + " select  " + stKey.Render("s") + " scan  " +
		stKey.Render("a") + " add by IP  " + stKey.Render("x") + " forget  " + stKey.Render("esc") + " close")
	return stModal.Render(b.String())
}

func deviceTitle(t string) string {
	if t == "" {
		return "Flexit unit"
	}
	return t
}
