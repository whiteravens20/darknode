// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

// Package legal holds the notices the user interface has to show.
package legal

// The attribution required by the additional term in NOTICE. A copy or a
// modified version keeps these two exactly; it may put "Based on" before them.
const (
	AttributionName = "DarkNode by White Ravens"
	AttributionURL  = "https://github.com/whiteravens20/darknode"
)

// SourceURL is where the source code of the running version can be had, as
// section 13 of the AGPL asks of a program used over a network. A fork points
// it at its own repository; the attribution above stays.
var SourceURL = "https://github.com/whiteravens20/darknode"

// Copyright is the copyright line of the original program.
const Copyright = "Copyright (C) 2026 White Ravens"

// LicenceURL is where the licence text can be read.
const LicenceURL = "https://www.gnu.org/licenses/agpl-3.0.html"

// Lines are the notices as the terminal's `about` command prints them.
func Lines() []string {
	return []string{
		AttributionName + " - " + AttributionURL,
		Copyright + ".",
		"This program is free software under the GNU Affero General Public License,",
		"version 3, with one additional term (see NOTICE). It comes with no warranty.",
		"Licence: " + LicenceURL,
		"Source of this version: " + SourceURL,
	}
}
