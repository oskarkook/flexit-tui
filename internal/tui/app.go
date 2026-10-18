// Package tui is the Bubble Tea interface.
package tui

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/oskarkook/flexit-tui/internal/config"
	"github.com/oskarkook/flexit-tui/internal/flexit"
)

const (
	pollInterval   = 5 * time.Second
	requestTimeout = 15 * time.Second
)

type modalKind int

const (
	modalNone modalKind = iota
	modalDevices
	modalConfirm
	modalInput
)

type Options struct {
	Config *config.State
	Scan   []netip.Prefix
}

type Model struct {
	opts Options
	cfg  *config.State

	width, height int
	spin          spinner.Model

	// discovery
	discovering bool
	discovered  bool
	online      map[string]bool

	// current unit
	current    string // config key
	gen        int    // bumped on every (re)connect; stale messages are dropped
	dev        *flexit.Device
	state      *flexit.State
	connecting bool
	polling    bool
	busy       string // label of a write in progress
	connErr    error
	pollErr    error

	// navigation
	tab     int
	cursor  map[int]int
	pending map[string]float64

	// modals
	modal         modalKind
	menuCursor    int
	confirmMsg    string
	confirmAction *pendingAction
	input         textinput.Model
	inputLabel    string
	inputFor      string // "address", or the id of the number row being edited

	flash     string
	flashErr  bool
	flashTime time.Time
}

func New(opts Options) Model {
	ti := textinput.New()
	ti.SetWidth(32)
	return Model{
		opts:    opts,
		cfg:     opts.Config,
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
		online:  map[string]bool{},
		cursor:  map[int]int{},
		pending: map[string]float64{},
		input:   ti,
	}
}

// ---------------------------------------------------------------- messages

type discoveredMsg struct {
	infos []flexit.Info
	err   error
}

type connectedMsg struct {
	gen int
	dev *flexit.Device
	err error
}

type polledMsg struct {
	gen   int
	state *flexit.State
	err   error
}

type tickMsg struct{ gen int }

type actionDoneMsg struct {
	gen   int
	label string
	err   error
}

type probedMsg struct {
	info flexit.Info
	err  error
}

// ---------------------------------------------------------------- commands

func (m *Model) knownAddrs() []netip.Addr {
	var out []netip.Addr
	for _, d := range m.cfg.Devices {
		if ap, err := netip.ParseAddrPort(d.Addr); err == nil {
			out = append(out, ap.Addr())
		}
	}
	return out
}

func (m *Model) discover() tea.Cmd {
	m.discovering = true
	known, extra := m.knownAddrs(), m.opts.Scan
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		infos, err := flexit.Discover(ctx, known, extra)
		return discoveredMsg{infos, err}
	}
}

// connect switches to a unit and starts talking to it.
func (m *Model) connect(key string) tea.Cmd {
	d := m.cfg.Find(key)
	if d == nil {
		return nil
	}
	m.gen++
	m.current = key
	m.dev, m.state = nil, nil
	m.connecting, m.polling = true, false
	m.connErr, m.pollErr = nil, nil
	m.pending = map[string]float64{}
	gen := m.gen
	addr, err := netip.ParseAddrPort(d.Addr)
	if err != nil {
		return func() tea.Msg { return connectedMsg{gen: gen, err: err} }
	}
	id, cache, cacheFor := d.DeviceID, d.Resolved, d.ResolvedFor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		dev, err := flexit.Connect(ctx, addr, id, cache, cacheFor)
		return connectedMsg{gen: gen, dev: dev, err: err}
	}
}

func (m *Model) poll() tea.Cmd {
	if m.dev == nil || m.polling {
		return nil
	}
	m.polling = true
	dev, gen := m.dev, m.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		s, err := dev.Poll(ctx)
		return polledMsg{gen: gen, state: s, err: err}
	}
}

func tick(gen int) tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{gen} })
}

func (m *Model) run(label string, apply applyFunc, v float64) tea.Cmd {
	if m.dev == nil {
		return nil
	}
	m.busy = label
	dev, st, gen := m.dev, m.state, m.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return actionDoneMsg{gen: gen, label: label, err: apply(ctx, dev, st, v)}
	}
}

func (m *Model) setFlash(msg string, isErr bool) {
	m.flash, m.flashErr, m.flashTime = msg, isErr, time.Now()
}

func (m *Model) save() {
	if err := m.cfg.Save(); err != nil {
		m.setFlash("Could not save settings: "+err.Error(), true)
	}
}

// ---------------------------------------------------------------- init

// Init shows a unit immediately when one is known: the last explicit choice,
// or else a random known unit. Discovery only runs when the user asks.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick}
	start := ""
	if m.cfg.Selected != "" && m.cfg.Find(m.cfg.Selected) != nil {
		start = m.cfg.Selected
	} else if len(m.cfg.Devices) > 0 {
		start = m.cfg.Devices[rand.IntN(len(m.cfg.Devices))].Key
	}
	if start != "" {
		cmds = append(cmds, func() tea.Msg { return startConnectMsg{start} })
	}
	return tea.Batch(cmds...)
}

// Init has a value receiver, so the initial connect is replayed through a
// message to mutate the model.
type startConnectMsg struct{ key string }

// ---------------------------------------------------------------- update

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case startConnectMsg:
		return m, m.connect(msg.key)

	case discoveredMsg:
		return m.onDiscovered(msg)

	case connectedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.connecting = false
		if msg.err != nil {
			m.connErr = msg.err
			return m, tick(m.gen) // retry
		}
		m.dev, m.connErr = msg.dev, nil
		if d := m.cfg.Find(m.current); d != nil {
			d.Title = msg.dev.Info.Title()
			d.Resolved, d.ResolvedFor = msg.dev.Resolved(), msg.dev.CacheTag()
			d.LastSeen = time.Now()
			m.save()
		}
		return m, m.poll()

	case polledMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.polling = false
		if msg.err != nil {
			m.pollErr = msg.err
		} else {
			m.state, m.pollErr = msg.state, nil
		}
		return m, tick(m.gen)

	case tickMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if m.dev == nil && !m.connecting {
			return m, m.connect(m.current)
		}
		return m, m.poll()

	case actionDoneMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.busy = ""
		if msg.err != nil {
			m.setFlash(msg.label+" failed: "+msg.err.Error(), true)
		} else {
			m.setFlash(msg.label+" ✓", false)
		}
		return m, m.poll()

	case probedMsg:
		m.busy = ""
		if msg.err != nil {
			m.setFlash("No Flexit unit answered: "+msg.err.Error(), true)
			return m, nil
		}
		key := m.remember(msg.info)
		m.online[key] = true
		m.cfg.Selected = key
		m.save()
		return m, m.connect(key)

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}

	if m.modal == modalInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) remember(info flexit.Info) string {
	m.cfg.Upsert(config.Device{
		Key:      info.Key(),
		Addr:     info.Addr.String(),
		DeviceID: info.DeviceID,
		Title:    info.Title(),
		Serial:   info.Serial,
		LastSeen: time.Now(),
	})
	return info.Key()
}

func (m Model) onDiscovered(msg discoveredMsg) (tea.Model, tea.Cmd) {
	m.discovering, m.discovered = false, true
	if msg.err != nil && len(msg.infos) == 0 {
		m.setFlash("Discovery failed: "+msg.err.Error(), true)
	}
	m.online = map[string]bool{}
	var cmd tea.Cmd
	for _, info := range msg.infos {
		before := m.cfg.Find(info.Key())
		moved := before != nil && before.Addr != info.Addr.String()
		key := m.remember(info)
		m.online[key] = true
		// The current unit changed address (DHCP): reconnect at the new one.
		if key == m.current && moved {
			cmd = m.connect(key)
		}
	}
	m.save()

	if m.current == "" && len(msg.infos) > 0 {
		key := ""
		if m.cfg.Selected != "" && m.online[m.cfg.Selected] {
			key = m.cfg.Selected
		} else {
			key = msg.infos[rand.IntN(len(msg.infos))].Key()
		}
		cmd = m.connect(key)
	}
	return m, cmd
}

// ---------------------------------------------------------------- keys

func (m Model) rows() []row {
	t := tabs[m.tab]
	if t.rows == nil || m.dev == nil || m.state == nil {
		return nil
	}
	return t.rows(m.dev, m.state)
}

func (m Model) selectedRow() (row, bool) {
	rows := m.rows()
	c := m.cursor[m.tab]
	if c < 0 || c >= len(rows) {
		return row{}, false
	}
	return rows[c], true
}

// moveCursor moves to the next interactive row in the given direction.
func (m *Model) moveCursor(dir int) {
	rows := m.rows()
	c := m.cursor[m.tab]
	for i := c + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].interactive() {
			m.cursor[m.tab] = i
			return
		}
	}
}

// fixCursor keeps the cursor on an interactive row as rows come and go.
func (m *Model) fixCursor() {
	rows := m.rows()
	c := m.cursor[m.tab]
	if c < len(rows) && c >= 0 && rows[c].interactive() {
		return
	}
	for i := min(c, len(rows)-1); i >= 0; i-- {
		if rows[i].interactive() {
			m.cursor[m.tab] = i
			return
		}
	}
	for i := range rows {
		if rows[i].interactive() {
			m.cursor[m.tab] = i
			return
		}
	}
}

func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.modal {
	case modalConfirm:
		switch key {
		case "y", "Y", "enter":
			m.modal = modalNone
			a := m.confirmAction
			m.confirmAction = nil
			if a == nil {
				return m, nil
			}
			return m, m.run(a.label, a.apply, a.v)
		case "n", "N", "esc", "q":
			m.modal = modalNone
			m.confirmAction = nil
		}
		return m, nil

	case modalInput:
		switch key {
		case "esc":
			m.modal = modalNone
			m.input.Blur()
			return m, nil
		case "enter":
			m.modal = modalNone
			m.input.Blur()
			return m.onInputDone(strings.TrimSpace(m.input.Value()))
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modalDevices:
		return m.onDeviceMenuKey(key)
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "u":
		m.openDeviceMenu()
		return m, nil
	case "s":
		if !m.discovering {
			m.setFlash("Scanning the network…", false)
			return m, m.discover()
		}
		return m, nil
	case "a":
		return m, m.askAddress()
	case "tab", "right", "l":
		if key != "tab" && m.tab != 0 {
			break
		}
		m.tab = (m.tab + 1) % len(tabs)
		m.fixCursor()
		return m, nil
	case "shift+tab", "left", "h":
		if key != "shift+tab" && m.tab != 0 {
			break
		}
		m.tab = (m.tab + len(tabs) - 1) % len(tabs)
		m.fixCursor()
		return m, nil
	case "1", "2", "3", "4", "5":
		m.tab = int(key[0]-'1') % len(tabs)
		m.fixCursor()
		return m, nil
	}

	if m.tab == 0 {
		return m, nil
	}
	m.fixCursor()
	r, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	pending, hasPending := m.pending[r.id]
	value := r.current
	if hasPending {
		value = pending
	}

	switch key {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "esc":
		delete(m.pending, r.id)
	case "left", "h", "-":
		m.adjust(r, value, -1)
	case "right", "l", "+", "=":
		m.adjust(r, value, 1)
	case "enter", "space":
		if m.busy != "" {
			m.setFlash("Busy: "+m.busy, true)
			return m, nil
		}
		switch r.kind {
		case kToggle:
			value = 1 - r.current
		case kChoice, kNumber:
			if !hasPending {
				if r.kind == kNumber {
					return m, m.askNumber(r)
				}
				return m, nil
			}
		}
		delete(m.pending, r.id)
		return m, m.applyRow(r, value)
	case "e":
		if r.kind == kNumber {
			return m, m.askNumber(r)
		}
	}
	return m, nil
}

func (m *Model) adjust(r row, value float64, dir float64) {
	switch r.kind {
	case kChoice:
		n := float64(len(r.options))
		v := value + dir
		if v < 0 {
			v = n - 1
		} else if v >= n {
			v = 0
		}
		m.setPending(r, v)
	case kNumber:
		v := value + dir*r.step
		// Snap to the step grid so odd current values line up.
		if r.step > 0 {
			v = float64(int64((v-r.min)/r.step+0.5))*r.step + r.min
			if dir > 0 && v <= value {
				v += r.step
			} else if dir < 0 && v >= value {
				v -= r.step
			}
		}
		m.setPending(r, r.clamp(v))
	case kToggle:
		m.setPending(r, 1-value)
	}
}

func (m *Model) setPending(r row, v float64) {
	if v == r.current {
		delete(m.pending, r.id)
	} else {
		m.pending[r.id] = v
	}
}

func (m *Model) applyRow(r row, v float64) tea.Cmd {
	label := strings.TrimSpace(r.label)
	if r.kind == kAction {
		label = r.button
	} else {
		label = fmt.Sprintf("%s → %s", label, r.format(v))
	}
	if r.confirm != nil {
		if q := r.confirm(v); q != "" {
			m.modal = modalConfirm
			m.confirmMsg = q
			m.confirmAction = &pendingAction{label: label, apply: r.apply, v: v}
			return nil
		}
	}
	return m.run(label, r.apply, v)
}

type pendingAction struct {
	label string
	apply applyFunc
	v     float64
}
