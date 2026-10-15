// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package world

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// The catalogue's addresses come only from ranges that are never routed on the
// public internet, so no fictional server is ever the address of a real host.
var reservedRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),      // RFC 1918
	netip.MustParsePrefix("172.16.0.0/12"),   // RFC 1918
	netip.MustParsePrefix("192.168.0.0/16"),  // RFC 1918
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598, shared address space
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC 5737, TEST-NET-1
	netip.MustParsePrefix("198.51.100.0/24"), // RFC 5737, TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC 5737, TEST-NET-3
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC 2544, benchmarking
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// The files' YAML shapes. Unknown keys are an error, so a typo in a data file
// fails the start-up instead of silently dropping a value.

type worldFile struct {
	Epoch time.Time `yaml:"epoch"`
	Start struct {
		Links    []string `yaml:"links"`
		Bank     string   `yaml:"bank"`
		Software []struct {
			ID      string `yaml:"id"`
			Version int    `yaml:"version"`
		} `yaml:"software"`
	} `yaml:"start"`
}

type orgFile struct {
	ID       string     `yaml:"id"`
	Name     string     `yaml:"name"`
	Type     ServerType `yaml:"type"`
	Faction  Faction    `yaml:"faction"`
	Location Point      `yaml:"location"`
	Servers  []struct {
		ID       string `yaml:"id"`
		Name     string `yaml:"name"`
		IP       string `yaml:"ip"`
		Security int    `yaml:"security"`
		Proxy    bool   `yaml:"proxy"`
		Firewall bool   `yaml:"firewall"`
		Monitor  bool   `yaml:"monitor"`
		Location *Point `yaml:"location"`
		Files    []struct {
			Path      string `yaml:"path"`
			SizeMB    int    `yaml:"size_mb"`
			Encrypted bool   `yaml:"encrypted"`
			Sensitive bool   `yaml:"sensitive"`
			Content   string `yaml:"content"`
		} `yaml:"files"`
	} `yaml:"servers"`
}

type softwareFile struct {
	Software []struct {
		ID          string `yaml:"id"`
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Versions    []struct {
			Version int `yaml:"version"`
			Price   int `yaml:"price"`
			SizeMB  int `yaml:"size_mb"`
			CPUMHz  int `yaml:"cpu_mhz"`
			RAMMB   int `yaml:"ram_mb"`
		} `yaml:"versions"`
	} `yaml:"software"`
}

// Load reads the catalogue from fsys: world.yaml, software.yaml and one file
// per organisation under orgs/. It returns every problem it finds, not just
// the first.
func Load(fsys fs.FS) (*World, error) {
	v := &validator{}
	w := &World{
		serverByID:   map[string]*Server{},
		serverByIP:   map[netip.Addr]*Server{},
		softwareByID: map[string]*Software{},
	}

	orgPaths, err := fs.Glob(fsys, "orgs/*.yaml")
	if err != nil {
		return nil, err
	}
	if len(orgPaths) == 0 {
		v.add("orgs/: no organisation files")
	}
	sort.Strings(orgPaths)
	orgIDs := map[string]string{}
	for _, p := range orgPaths {
		var of orgFile
		if err := decode(fsys, p, &of); err != nil {
			v.add("%v", err)
			continue
		}
		v.org(w, orgIDs, p, &of)
	}

	var sf softwareFile
	if err := decode(fsys, "software.yaml", &sf); err != nil {
		v.add("%v", err)
	} else {
		v.software(w, &sf)
	}

	var wf worldFile
	if err := decode(fsys, "world.yaml", &wf); err != nil {
		v.add("%v", err)
	} else {
		v.start(w, &wf)
	}

	if err := v.err(); err != nil {
		return nil, err
	}

	sort.Slice(w.Orgs, func(i, j int) bool { return w.Orgs[i].ID < w.Orgs[j].ID })
	sort.Slice(w.Servers, func(i, j int) bool {
		if w.Servers[i].Name != w.Servers[j].Name {
			return w.Servers[i].Name < w.Servers[j].Name
		}
		return w.Servers[i].ID < w.Servers[j].ID
	})
	sort.Slice(w.Software, func(i, j int) bool { return w.Software[i].Name < w.Software[j].Name })
	return w, nil
}

func decode(fsys fs.FS, name string, out any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: more than one YAML document", name)
	}
	return nil
}

type validator struct{ problems []string }

func (v *validator) add(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	return fmt.Errorf("world catalogue has %d problem(s):\n  %s", len(v.problems), strings.Join(v.problems, "\n  "))
}

func (v *validator) org(w *World, orgIDs map[string]string, file string, of *orgFile) {
	where := file
	if !idPattern.MatchString(of.ID) {
		v.add("%s: id %q must be lower-case letters, digits and single hyphens", where, of.ID)
	}
	if want := of.ID + ".yaml"; path.Base(file) != want {
		v.add("%s: the file of organisation %q must be named %s", where, of.ID, want)
	}
	if other, dup := orgIDs[of.ID]; dup {
		v.add("%s: organisation id %q is also used in %s", where, of.ID, other)
	}
	orgIDs[of.ID] = file
	if strings.TrimSpace(of.Name) == "" {
		v.add("%s: name is empty", where)
	}
	if !serverTypes[of.Type] {
		v.add("%s: unknown type %q", where, of.Type)
	}
	if !factions[of.Faction] {
		v.add("%s: unknown faction %q", where, of.Faction)
	}
	v.point(where, "location", of.Location)
	if len(of.Servers) == 0 {
		v.add("%s: an organisation needs at least one server", where)
	}

	org := &Org{ID: of.ID, Name: of.Name, Type: of.Type, Faction: of.Faction, Location: of.Location}
	for i, s := range of.Servers {
		at := fmt.Sprintf("%s: servers[%d]", where, i)
		if !idPattern.MatchString(s.ID) {
			v.add("%s: id %q must be lower-case letters, digits and single hyphens", at, s.ID)
		}
		if _, dup := w.serverByID[s.ID]; dup {
			v.add("%s: server id %q is used twice", at, s.ID)
		}
		ip, err := netip.ParseAddr(s.IP)
		switch {
		case err != nil || !ip.Is4():
			v.add("%s: ip %q is not an IPv4 address", at, s.IP)
		case !reserved(ip):
			v.add("%s: ip %s is a public internet address; the world uses reserved ranges only", at, ip)
		default:
			if other, dup := w.serverByIP[ip]; dup {
				v.add("%s: ip %s is also the address of %s", at, ip, other.ID)
			}
		}
		if s.Security < 1 || s.Security > 10 {
			v.add("%s: security %d must be from 1 to 10", at, s.Security)
		}
		name := s.Name
		if name == "" {
			name = of.Name
		}
		loc := of.Location
		if s.Location != nil {
			loc = *s.Location
			v.point(at, "location", loc)
		}
		srv := &Server{
			ID: s.ID, Name: name, IP: ip, Org: org, Security: s.Security,
			Proxy: s.Proxy, Firewall: s.Firewall, Monitor: s.Monitor, Location: loc,
		}
		paths := map[string]bool{}
		for j, f := range s.Files {
			fat := fmt.Sprintf("%s.files[%d]", at, j)
			if !strings.HasPrefix(f.Path, "/") || path.Clean(f.Path) != f.Path || f.Path == "/" {
				v.add("%s: path %q must be an absolute, clean file path", fat, f.Path)
			}
			if paths[f.Path] {
				v.add("%s: path %q appears twice on the server", fat, f.Path)
			}
			paths[f.Path] = true
			if f.SizeMB < 1 {
				v.add("%s: size_mb %d must be at least 1", fat, f.SizeMB)
			}
			srv.Files = append(srv.Files, File{
				Path: f.Path, SizeMB: f.SizeMB, Encrypted: f.Encrypted,
				Sensitive: f.Sensitive, Content: f.Content,
			})
		}
		sort.Slice(srv.Files, func(a, b int) bool { return srv.Files[a].Path < srv.Files[b].Path })
		org.Servers = append(org.Servers, srv)
		w.Servers = append(w.Servers, srv)
		if s.ID != "" {
			w.serverByID[s.ID] = srv
		}
		if ip.IsValid() {
			w.serverByIP[ip] = srv
		}
	}
	w.Orgs = append(w.Orgs, org)
}

func (v *validator) point(where, field string, p Point) {
	if p.X < 0 || p.X > 100 || p.Y < 0 || p.Y > 100 {
		v.add("%s: %s (%g, %g) must lie within 0..100", where, field, p.X, p.Y)
	}
}

func (v *validator) software(w *World, sf *softwareFile) {
	if len(sf.Software) == 0 {
		v.add("software.yaml: no software")
	}
	for i, s := range sf.Software {
		at := fmt.Sprintf("software.yaml: software[%d]", i)
		if !regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`).MatchString(s.ID) {
			v.add("%s: id %q must be lower-case letters, digits and single underscores", at, s.ID)
		}
		if _, dup := w.softwareByID[s.ID]; dup {
			v.add("%s: id %q is used twice", at, s.ID)
		}
		if strings.TrimSpace(s.Name) == "" {
			v.add("%s: name is empty", at)
		}
		if len(s.Versions) == 0 {
			v.add("%s: no versions", at)
		}
		sw := &Software{ID: s.ID, Name: s.Name, Description: s.Description}
		for j, sv := range s.Versions {
			vat := fmt.Sprintf("%s.versions[%d]", at, j)
			if sv.Version != j+1 {
				v.add("%s: version %d must be %d (versions run 1, 2, 3 in order)", vat, sv.Version, j+1)
			}
			if sv.Price < 0 || sv.SizeMB < 1 || sv.CPUMHz < 0 || sv.RAMMB < 0 {
				v.add("%s: price, cpu_mhz and ram_mb must not be negative and size_mb must be at least 1", vat)
			}
			sw.Versions = append(sw.Versions, SoftwareVersion{
				Version: sv.Version, Price: sv.Price, SizeMB: sv.SizeMB, CPUMHz: sv.CPUMHz, RAMMB: sv.RAMMB,
			})
		}
		w.Software = append(w.Software, sw)
		w.softwareByID[s.ID] = sw
	}
}

func (v *validator) start(w *World, wf *worldFile) {
	if wf.Epoch.IsZero() {
		v.add("world.yaml: epoch is required")
	}
	w.Epoch = wf.Epoch.UTC()
	seen := map[string]bool{}
	for _, id := range wf.Start.Links {
		s, ok := w.serverByID[id]
		if !ok {
			v.add("world.yaml: start.links names unknown server %q", id)
			continue
		}
		if seen[id] {
			v.add("world.yaml: start.links names %q twice", id)
			continue
		}
		seen[id] = true
		w.Start.Links = append(w.Start.Links, s)
	}
	bank, ok := w.serverByID[wf.Start.Bank]
	switch {
	case !ok:
		v.add("world.yaml: start.bank names unknown server %q", wf.Start.Bank)
	case bank.Type() != TypeBank:
		v.add("world.yaml: start.bank %q is not a bank", wf.Start.Bank)
	case !seen[bank.ID]:
		v.add("world.yaml: start.bank %q must also be in start.links", wf.Start.Bank)
	default:
		w.Start.Bank = bank
	}
	for _, ref := range wf.Start.Software {
		sw, ok := w.softwareByID[ref.ID]
		if !ok {
			v.add("world.yaml: start.software names unknown program %q", ref.ID)
			continue
		}
		if _, ok := sw.Version(ref.Version); !ok {
			v.add("world.yaml: program %q has no version %d", ref.ID, ref.Version)
			continue
		}
		w.Start.Software = append(w.Start.Software, SoftwareRef{ID: ref.ID, Version: ref.Version})
	}
}

func reserved(ip netip.Addr) bool {
	for _, p := range reservedRanges {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
