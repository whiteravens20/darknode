// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package world holds the world catalogue: the organisations, servers, files,
// people and software that every player shares. The catalogue is read once at
// start-up from data files and never changes while the server runs; what a
// player does to the world is stored per run, beside it.
package world

import (
	"net/netip"
	"time"
)

// ServerType is the kind of organisation a server belongs to.
type ServerType string

// The server types of the catalogue.
const (
	TypeInterNIC         ServerType = "INTERNIC"
	TypeNews             ServerType = "NEWS"
	TypeISP              ServerType = "ISP"
	TypeUniversity       ServerType = "UNIVERSITY"
	TypeCorporate        ServerType = "CORPORATE"
	TypeBank             ServerType = "BANK"
	TypeStockMarket      ServerType = "STOCK_MARKET"
	TypeGovernment       ServerType = "GOVERNMENT"
	TypeSocialSecurity   ServerType = "SOCIAL_SECURITY"
	TypeCriminalDatabase ServerType = "CRIMINAL_DATABASE"
	TypeDarkNodeTest     ServerType = "DARKNODE_TEST"
	TypeDarkNodeInternal ServerType = "DARKNODE_INTERNAL"
	TypeLANHost          ServerType = "LAN_HOST"
)

var serverTypes = map[ServerType]bool{
	TypeInterNIC: true, TypeNews: true, TypeISP: true, TypeUniversity: true,
	TypeCorporate: true, TypeBank: true, TypeStockMarket: true, TypeGovernment: true,
	TypeSocialSecurity: true, TypeCriminalDatabase: true, TypeDarkNodeTest: true,
	TypeDarkNodeInternal: true, TypeLANHost: true,
}

// Public reports whether a plain connection to a server of this type starts
// no trace: InterNIC and the news servers are there to be read.
func (t ServerType) Public() bool {
	return t == TypeInterNIC || t == TypeNews
}

// Faction is one of the four factions an organisation can belong to.
type Faction string

// The factions.
const (
	FactionNone        Faction = ""
	FactionCorporate   Faction = "CORPORATE"
	FactionGovernment  Faction = "GOVERNMENT"
	FactionUnderground Faction = "UNDERGROUND"
	FactionAcademic    Faction = "ACADEMIC"
)

var factions = map[Faction]bool{
	FactionNone: true, FactionCorporate: true, FactionGovernment: true,
	FactionUnderground: true, FactionAcademic: true,
}

// Point is a position on the world map, each coordinate from 0 to 100.
type Point struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

// File is a file on a server.
type File struct {
	Path      string
	SizeMB    int
	Encrypted bool
	Sensitive bool
	Content   string
}

// Server is a machine a player can connect to.
type Server struct {
	ID       string
	Name     string
	IP       netip.Addr
	Org      *Org
	Security int
	Proxy    bool
	Firewall bool
	Monitor  bool
	Location Point
	Files    []File
}

// Type is the type of the server's organisation.
func (s *Server) Type() ServerType { return s.Org.Type }

// Org is an organisation of the world.
type Org struct {
	ID       string
	Name     string
	Type     ServerType
	Faction  Faction
	Location Point
	Servers  []*Server
}

// SoftwareVersion is one purchasable version of a program.
type SoftwareVersion struct {
	Version int
	Price   int
	SizeMB  int
	CPUMHz  int
	RAMMB   int
}

// Software is a program the player can own and run.
type Software struct {
	ID          string
	Name        string
	Description string
	Versions    []SoftwareVersion
}

// Version returns the given version of the program, if it exists.
func (s *Software) Version(v int) (SoftwareVersion, bool) {
	for _, sv := range s.Versions {
		if sv.Version == v {
			return sv, true
		}
	}
	return SoftwareVersion{}, false
}

// SoftwareRef names one version of one program.
type SoftwareRef struct {
	ID      string
	Version int
}

// Start is what a new run begins with.
type Start struct {
	// Links are the servers whose addresses the player knows from the start.
	Links []*Server
	// Bank is the bank that holds the player's first account.
	Bank *Server
	// Software is installed on the gateway of a new run.
	Software []SoftwareRef
}

// World is the whole catalogue.
type World struct {
	// Epoch is the world date at the moment the instance was first started.
	Epoch time.Time
	// Orgs are sorted by ID.
	Orgs []*Org
	// Servers are sorted by name, then ID.
	Servers []*Server
	// Software is sorted by name.
	Software []*Software
	Start    Start

	serverByID   map[string]*Server
	serverByIP   map[netip.Addr]*Server
	softwareByID map[string]*Software
}

// ServerByID returns the server with the given ID.
func (w *World) ServerByID(id string) (*Server, bool) {
	s, ok := w.serverByID[id]
	return s, ok
}

// ServerByIP returns the server with the given address.
func (w *World) ServerByIP(ip netip.Addr) (*Server, bool) {
	s, ok := w.serverByIP[ip]
	return s, ok
}

// SoftwareByID returns the program with the given ID.
func (w *World) SoftwareByID(id string) (*Software, bool) {
	s, ok := w.softwareByID[id]
	return s, ok
}
