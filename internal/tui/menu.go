package tui

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/oskarkook/flexit-tui/internal/bacnet"
	"github.com/oskarkook/flexit-tui/internal/flexit"
)

func (m *Model) openDeviceMenu() {
	m.modal = modalDevices
	m.menuCursor = 0
	for i, d := range m.cfg.Devices {
		if d.Key == m.current {
			m.menuCursor = i
		}
	}
}

func (m Model) onDeviceMenuKey(key string) (tea.Model, tea.Cmd) {
	n := len(m.cfg.Devices)
	switch key {
	case "esc", "u", "q":
		m.modal = modalNone
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < n-1 {
			m.menuCursor++
		}
	case "enter", "space":
		if m.menuCursor < n {
			key := m.cfg.Devices[m.menuCursor].Key
			m.modal = modalNone
			m.cfg.Selected = key
			m.save()
			if key != m.current || m.dev == nil {
				return m, m.connect(key)
			}
		}
	case "s":
		if !m.discovering {
			m.modal = modalNone
			m.setFlash("Scanning the network…", false)
			return m, m.discover()
		}
	case "a":
		m.modal = modalNone
		return m, m.askAddress()
	case "x":
		if m.menuCursor < n {
			key := m.cfg.Devices[m.menuCursor].Key
			m.cfg.Remove(key)
			m.save()
			if key == m.current {
				m.gen++
				m.current, m.dev, m.state = "", nil, nil
				m.connecting, m.connErr, m.pollErr = false, nil, nil
			}
			if m.menuCursor >= len(m.cfg.Devices) && m.menuCursor > 0 {
				m.menuCursor--
			}
		}
	}
	return m, nil
}

func (m *Model) askAddress() tea.Cmd {
	m.modal = modalInput
	m.inputFor = "address"
	m.inputLabel = "Unit IP address (optionally :port)"
	m.input.Placeholder = "192.168.1.50"
	m.input.SetValue("")
	return m.input.Focus()
}

func (m *Model) askNumber(r row) tea.Cmd {
	m.modal = modalInput
	m.inputFor = r.id
	m.inputLabel = fmt.Sprintf("%s (%s – %s)", strings.TrimSpace(r.label), r.format(r.min), r.format(r.max))
	m.input.Placeholder = ""
	m.input.SetValue(strconv.FormatFloat(r.current, 'f', r.decimals, 64))
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m Model) onInputDone(text string) (tea.Model, tea.Cmd) {
	if text == "" {
		return m, nil
	}
	if m.inputFor == "address" {
		ap, err := netip.ParseAddrPort(text)
		if err != nil {
			a, err2 := netip.ParseAddr(text)
			if err2 != nil {
				m.setFlash("Not an IP address: "+text, true)
				return m, nil
			}
			ap = netip.AddrPortFrom(a, bacnet.DefaultPort)
		}
		m.busy = "Looking for a unit at " + ap.String()
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			info, err := flexit.Probe(ctx, ap)
			return probedMsg{info, err}
		}
	}

	for _, r := range m.rows() {
		if r.id != m.inputFor || r.kind != kNumber {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.ReplaceAll(text, ",", "."), r.unit), 64)
		if err != nil {
			m.setFlash("Not a number: "+text, true)
			return m, nil
		}
		if v < r.min || v > r.max {
			m.setFlash(fmt.Sprintf("%s must be between %s and %s", strings.TrimSpace(r.label), r.format(r.min), r.format(r.max)), true)
			return m, nil
		}
		if m.busy != "" {
			m.setFlash("Busy: "+m.busy, true)
			return m, nil
		}
		delete(m.pending, r.id)
		return m, m.applyRow(r, v)
	}
	return m, nil
}
