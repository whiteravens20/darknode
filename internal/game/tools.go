// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// Layer is one of the defences in front of a server.
type Layer string

// The layers, in the order they have to be opened. A monitor stands outside
// that order: it watches rather than blocks, and masking it is worth doing
// first because it is what makes a trace fast.
const (
	LayerProxy    Layer = "proxy"
	LayerFirewall Layer = "firewall"
	LayerPassword Layer = "password"
	LayerMonitor  Layer = "monitor"
)

// entryLayers are the layers that have to be opened in this order.
var entryLayers = []Layer{LayerProxy, LayerFirewall, LayerPassword}

// toolLayer says which layer each program opens. A program that is not here
// does something else — copying a file, clearing a log — and is run the same
// way.
var toolLayer = map[string]Layer{
	"proxy_opener":    LayerProxy,
	"firewall_opener": LayerFirewall,
	"pass_breaker":    LayerPassword,
	"monitor_mask":    LayerMonitor,
}

// Why a program is refused. The server decides every one of these.
var (
	ErrToolNotInstalled = errors.New("the gateway has no such program")
	ErrToolRunning      = errors.New("that program is already running here")
	ErrLayerOpen        = errors.New("that layer is open already")
	ErrLayerAbsent      = errors.New("the server has no such layer")
	ErrLayerOutOfOrder  = errors.New("an outer layer is still in the way")
	ErrTooManyTools     = errors.New("the gateway's memory has no room for another program")
	ErrNotEnoughCPU     = errors.New("the gateway's processor is too slow for that program")
	ErrNotEnoughRAM     = errors.New("the gateway has too little memory for that program")
	ErrToolNotRunning   = errors.New("that program is not running")
)

// running is one program at work on a connection.
type running struct {
	// Software is the program's id, and Version the installed version.
	Software string
	Version  int
	Name     string
	// Layer is what it opens, or empty for a program that does something else.
	Layer Layer
	// work is the processor time it needs, in MHz·seconds, and done is how
	// much of it the gateway has given it.
	work, done float64
}

// ToolTime is how long a program takes: the work of one level of security for
// each level the server has, divided by the share of the processor it gets.
// Nothing here has a floor, so a faster processor always helps.
func ToolTime(security, version, cpuMHz, parallel int) time.Duration {
	if cpuMHz <= 0 || parallel < 1 {
		return 0
	}
	work := toolWork(security, version)
	seconds := work / (float64(cpuMHz) / float64(parallel))
	return time.Duration(seconds * float64(time.Second))
}

// toolWork is the processor time a program needs, in MHz·seconds.
func toolWork(security, version int) float64 {
	return BaseTool.Seconds() * float64(security) * VersionFactor(version) * CPURef
}

// maxParallel is how many programs the gateway's memory lets run at once.
func maxParallel(g store.Gateway) int {
	return max(g.RAMMB/RAMPerSlot, 1)
}

// StartTool sets a program to work on the player's connection. It is refused
// unless the layer it opens is the next one in the way, because the order is
// the server's, not the browser's.
func (e *Engine) StartTool(account int64, run *store.Run, sw *world.Software, version int) (*running, error) {
	installed := false
	for _, i := range run.Software {
		if i.Software == sw.ID && i.Version == version {
			installed = true
		}
	}
	if !installed {
		return nil, ErrToolNotInstalled
	}
	v, ok := sw.Version(version)
	if !ok {
		return nil, ErrToolNotInstalled
	}
	g := run.Gateway
	if v.CPUMHz > g.CPUMHz {
		return nil, ErrNotEnoughCPU
	}
	if v.RAMMB > g.RAMMB {
		return nil, ErrNotEnoughRAM
	}

	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		e.mu.Unlock()
		return nil, ErrNoConnection
	}
	conn := r.conn
	if len(conn.tools) >= maxParallel(g) {
		e.mu.Unlock()
		return nil, ErrTooManyTools
	}
	for _, t := range conn.tools {
		if t.Software == sw.ID {
			e.mu.Unlock()
			return nil, ErrToolRunning
		}
	}
	layer := toolLayer[sw.ID]
	if layer != "" {
		if err := conn.layerReady(layer); err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	tool := &running{
		Software: sw.ID, Version: version, Name: sw.Name, Layer: layer,
		work: toolWork(conn.Target.Security, version),
	}
	conn.tools = append(conn.tools, tool)
	conn.reported = e.env.Now()
	view := conn.viewWithCPU(conn.cpuMHz)
	e.mu.Unlock()

	e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: view})
	// Working on the way in is an act against the server, so it is what
	// starts the trace. Masking the monitor is not: it is preparation, and
	// doing it first is what holds the trace to its slower rate.
	if layer == LayerProxy || layer == LayerFirewall || layer == LayerPassword {
		e.ArmTrace(account, run)
	}
	started := *tool
	return &started, nil
}

// StopTool takes a program off the connection, giving its share of the
// processor back to the others. What it had done is lost.
func (e *Engine) StopTool(account int64, id string) error {
	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		e.mu.Unlock()
		return ErrNoConnection
	}
	conn := r.conn
	found := false
	kept := conn.tools[:0]
	for _, t := range conn.tools {
		if t.Software == id {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	conn.tools = kept
	view := conn.viewWithCPU(conn.cpuMHz)
	e.mu.Unlock()

	if !found {
		return ErrToolNotRunning
	}
	e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: view})
	return nil
}

// layerReady reports whether a layer may be worked on now: it has to be one
// the server has, still closed, and with nothing outside it left to open.
// The caller holds the engine's lock.
func (c *Connection) layerReady(layer Layer) error {
	if !c.has(layer) {
		return ErrLayerAbsent
	}
	if c.open[layer] {
		return ErrLayerOpen
	}
	if layer == LayerMonitor {
		return nil // outside the order: mask it whenever you like
	}
	for _, outer := range entryLayers {
		if outer == layer {
			return nil
		}
		if c.has(outer) && !c.open[outer] {
			return ErrLayerOutOfOrder
		}
	}
	return nil
}

// has reports whether the target has the layer at all. Every server has a
// password; the rest it has or it has not.
func (c *Connection) has(layer Layer) bool {
	switch layer {
	case LayerProxy:
		return c.Target.Proxy
	case LayerFirewall:
		return c.Target.Firewall
	case LayerMonitor:
		return c.Target.Monitor
	case LayerPassword:
		return true
	}
	return false
}

// nextLayer names the layer that has to be opened next, or empty once the way
// in is clear.
func (c *Connection) nextLayer() Layer {
	for _, layer := range entryLayers {
		if c.has(layer) && !c.open[layer] {
			return layer
		}
	}
	return ""
}

// closeOneLayer shuts one of the layers the player had opened, which is what
// an administrator does when a trace is well on its way. The password layer
// goes with the others: the run keeps the root it was written down for, but
// on this connection the way in has to be opened again.
//
// Which one it is, is the server's choice, not the player's. The caller holds
// the engine's lock.
func (c *Connection) closeOneLayer() Layer {
	var open []Layer
	for _, layer := range []Layer{LayerProxy, LayerFirewall, LayerPassword, LayerMonitor} {
		if c.has(layer) && c.open[layer] {
			open = append(open, layer)
		}
	}
	if len(open) == 0 {
		return ""
	}
	layer := open[rand.IntN(len(open))]
	c.open[layer] = false
	if layer == LayerPassword {
		c.Root = false
	}
	// A program working on that layer has lost its ground.
	kept := c.tools[:0]
	for _, tool := range c.tools {
		if tool.Layer == layer {
			continue
		}
		kept = append(kept, tool)
	}
	c.tools = kept
	return layer
}

// advanceTools gives every running program its share of the processor for dt
// and reports what finished. The caller holds the engine's lock.
func (c *Connection) advanceTools(dt time.Duration, cpuMHz int) []*running {
	if len(c.tools) == 0 || c.State != StateConnected {
		return nil
	}
	share := float64(cpuMHz) / float64(len(c.tools)) * dt.Seconds()
	var done []*running
	kept := make([]*running, 0, len(c.tools))
	for _, t := range c.tools {
		t.done += share
		if t.done < t.work {
			kept = append(kept, t)
			continue
		}
		if t.Layer != "" {
			if c.open == nil {
				c.open = map[Layer]bool{}
			}
			c.open[t.Layer] = true
			if t.Layer == LayerPassword {
				c.Root = true
			}
		}
		done = append(done, t)
	}
	c.tools = kept
	return done
}

// toolViews describes the running programs to the browser.
func (c *Connection) toolViews(cpuMHz int) []ToolView {
	out := make([]ToolView, 0, len(c.tools))
	for _, t := range c.tools {
		progress := 0.0
		if t.work > 0 {
			progress = math.Min(t.done/t.work, 1)
		}
		left := time.Duration(0)
		if cpuMHz > 0 && len(c.tools) > 0 {
			seconds := (t.work - t.done) / (float64(cpuMHz) / float64(len(c.tools)))
			left = time.Duration(max(seconds, 0) * float64(time.Second))
		}
		out = append(out, ToolView{
			ID: t.Software, Name: t.Name, Version: t.Version,
			Progress: math.Round(progress*1000) / 1000,
			Left:     left.Round(100 * time.Millisecond).Seconds(),
		})
	}
	return out
}

// layerViews describes the target's defences and what is open.
func (c *Connection) layerViews() []LayerView {
	var out []LayerView
	for _, layer := range []Layer{LayerProxy, LayerFirewall, LayerPassword, LayerMonitor} {
		if !c.has(layer) {
			continue
		}
		out = append(out, LayerView{Name: string(layer), Open: c.open[layer]})
	}
	return out
}
