// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package game

import (
	"errors"
	"net/netip"
	"time"

	"github.com/whiteravens20/darknode/internal/store"
	"github.com/whiteravens20/darknode/internal/world"
)

// ConnState is where a connection stands.
type ConnState string

// The states a connection passes through.
const (
	// StateConnecting is the handshake.
	StateConnecting ConnState = "CONNECTING"
	// StateConnected is a connection the player can work through.
	StateConnected ConnState = "CONNECTED"
)

// Why a connection or a route is refused. The server decides all of this:
// the browser may show a route, but it never says which one is allowed.
var (
	ErrNoConnection       = errors.New("not connected to anything")
	ErrAlreadyConnected   = errors.New("already connected")
	ErrUnknownAddress     = errors.New("the run does not know that address")
	ErrRouteFull          = errors.New("the gateway has no network card left for another relay")
	ErrPublicRelay        = errors.New("a public service does not pass a break-in through")
	ErrRelayAlreadyOnIt   = errors.New("that relay is already on the route")
	ErrTargetIsOnTheRoute = errors.New("the target is a relay of its own route")
)

// Connection is a live session with one server, reached through a route.
// It is live state: it lives in the simulation loop for as long as the player
// is online, and no restart of the server survives it, exactly as a dropped
// line would not.
type Connection struct {
	// Target is the server at the far end.
	Target *world.Server
	// Route are the relays the connection is bounced through, from the one
	// nearest the gateway to the one nearest the target.
	Route []*world.Server
	State ConnState
	// Root says whether the password layer has been opened, which is the
	// only thing that gives root. A public service needs no break-in to
	// read what it publishes, but what it marks sensitive still sits behind
	// root like anywhere else.
	Root bool
	// open says which of the target's layers have been opened. They belong
	// to the connection: leaving closes them again, and coming back means
	// opening them from the start.
	open map[Layer]bool
	// tools are the programs at work on this connection, sharing the
	// gateway's processor between them.
	tools []*running
	// cpuMHz is the processor the programs share. It is taken when the
	// connection opens: a gateway is not rebuilt mid-connection.
	cpuMHz int
	// opened is when the handshake started, and reported is when the browser
	// was last told how far the programs had got.
	opened, reported time.Time
}

// Route returns the relays chosen for the next connection.
func (e *Engine) Route(account int64) []*world.Server {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil {
		return nil
	}
	return append([]*world.Server(nil), r.route...)
}

// AddRelay puts one more relay on the route of the player's next connection.
// maxBounces is what the gateway's network cards allow.
func (e *Engine) AddRelay(account int64, relay *world.Server, maxBounces int) error {
	if relay.Type().Public() {
		return ErrPublicRelay
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.liveLocked(account)
	if len(r.route) >= maxBounces {
		return ErrRouteFull
	}
	for _, on := range r.route {
		if on == relay {
			return ErrRelayAlreadyOnIt
		}
	}
	r.route = append(r.route, relay)
	return nil
}

// ClearRoute forgets the route of the next connection.
func (e *Engine) ClearRoute(account int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r := e.live[account]; r != nil {
		r.route = nil
	}
}

// Connect opens a connection to the target through the route built so far,
// with the gateway that will work through it. A connection to a public
// service is up at once and open; anything else takes the handshake and
// begins without root.
func (e *Engine) Connect(account int64, target *world.Server, g store.Gateway) (*Connection, error) {
	now := e.env.Now()

	e.mu.Lock()
	r := e.liveLocked(account)
	if r.conn != nil {
		e.mu.Unlock()
		return nil, ErrAlreadyConnected
	}
	if len(r.route) > g.NICCount {
		e.mu.Unlock()
		return nil, ErrRouteFull
	}
	for _, relay := range r.route {
		if relay == target {
			e.mu.Unlock()
			return nil, ErrTargetIsOnTheRoute
		}
	}
	conn := &Connection{
		Target: target,
		Route:  append([]*world.Server(nil), r.route...),
		State:  StateConnecting,
		open:   map[Layer]bool{},
		cpuMHz: g.CPUMHz,
		opened: now,
	}
	// A public service answers at once — there is no door to knock on — but
	// it is not rooted by being read.
	if target.Type().Public() {
		conn.State = StateConnected
	}
	r.conn = conn
	view := conn.view()
	e.mu.Unlock()

	e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: view})
	return conn, nil
}

// Disconnect closes the player's connection. The layers that were opened on
// the target are forgotten with it, so coming back means opening them again.
func (e *Engine) Disconnect(account int64) (*Connection, error) {
	e.mu.Lock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		e.mu.Unlock()
		return nil, ErrNoConnection
	}
	conn := r.conn
	r.conn = nil
	e.mu.Unlock()

	e.out.Publish(account, DeltaConnection, ConnectionDelta{Connection: nil})
	return conn, nil
}

// Connection returns the player's live connection, or nil.
func (e *Engine) Connection(account int64) *Connection {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		return nil
	}
	copied := *r.conn
	return &copied
}

// ConnectionView returns what the browser is shown about the connection.
func (e *Engine) ConnectionView(account int64) *ConnectionView {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.live[account]
	if r == nil || r.conn == nil {
		return nil
	}
	return r.conn.viewWithCPU(r.conn.cpuMHz)
}

// liveLocked returns the account's live state, making it if the account has
// none. A command can arrive before the browser's stream has opened, and a
// route built then must not be dropped. The caller holds the lock.
func (e *Engine) liveLocked(account int64) *liveRun {
	r := e.live[account]
	if r == nil {
		r = &liveRun{offlineAt: e.env.Now()}
		e.live[account] = r
	}
	return r
}

// view is what a browser is told about a connection. The processor the
// programs share is the gateway's, which the caller knows; a view built
// without it still carries everything but the time left.
func (c *Connection) view() *ConnectionView {
	return c.viewWithCPU(0)
}

// viewWithCPU is the view with the time the running programs have left, which
// depends on the gateway sharing its processor between them.
func (c *Connection) viewWithCPU(cpuMHz int) *ConnectionView {
	v := &ConnectionView{
		Target: serverView(c.Target),
		State:  string(c.State),
		Root:   c.Root,
		Route:  make([]ServerView, 0, len(c.Route)),
		Layers: c.layerViews(),
		Tools:  c.toolViews(cpuMHz),
	}
	for _, relay := range c.Route {
		v.Route = append(v.Route, serverView(relay))
	}
	return v
}

// addr parses a player's address, which is always an IPv4 address of the
// world's reserved ranges.
func addr(text string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(text)
	if err != nil || !ip.Is4() {
		return netip.Addr{}, false
	}
	return ip, true
}
