// Command flexit is a terminal UI for Flexit Nordic ventilation units on the
// local network (BACnet/IP).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/oskarkook/flexit-tui/internal/config"
	"github.com/oskarkook/flexit-tui/internal/flexit"
	"github.com/oskarkook/flexit-tui/internal/tui"
)

func main() {
	defaultPath, _ := config.Path()
	statePath := flag.String("state", defaultPath, "file remembering known units and the last selection")
	scan := flag.String("scan", os.Getenv("FLEXIT_SCAN"), "extra comma-separated networks to scan, e.g. 192.168.5.0/24 (env FLEXIT_SCAN)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: flexit [flags]           interactive UI\n"+
			"       flexit [flags] discover  list units on the network\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var prefixes []netip.Prefix
	for _, s := range strings.Split(*scan, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			if a, err2 := netip.ParseAddr(s); err2 == nil {
				p = netip.PrefixFrom(a, 32)
			} else {
				fmt.Fprintf(os.Stderr, "flexit: bad -scan network %q: %v\n", s, err)
				os.Exit(2)
			}
		}
		prefixes = append(prefixes, p)
	}

	cfg, err := config.Load(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flexit: ignoring unreadable %s: %v\n", *statePath, err)
	}

	if flag.Arg(0) == "discover" {
		os.Exit(discover(cfg, prefixes))
	}

	if _, err := tea.NewProgram(tui.New(tui.Options{Config: cfg, Scan: prefixes})).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "flexit:", err)
		os.Exit(1)
	}
}

func discover(cfg *config.State, extra []netip.Prefix) int {
	var known []netip.Addr
	for _, d := range cfg.Devices {
		if ap, err := netip.ParseAddrPort(d.Addr); err == nil {
			known = append(known, ap.Addr())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	infos, err := flexit.Discover(ctx, known, extra)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flexit:", err)
		return 1
	}
	if len(infos) == 0 {
		fmt.Println("No units found.")
		return 1
	}
	for _, i := range infos {
		fmt.Printf("%-22s device %-4d %-20s %s\n", i.Addr, i.DeviceID, i.Title(), i.Serial)
	}
	return 0
}
