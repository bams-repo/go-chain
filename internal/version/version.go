// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package version

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bams-repo/fairchain/internal/coinparams"
)

const (
	// Major is the major version component (breaking protocol changes).
	Major = 0

	// Minor is the minor version component (new features, backward-compatible).
	Minor = 10

	// Patch is the patch version component (bug fixes).
	Patch = 4

	// ProtocolVersion is the peer-to-peer wire protocol version.
	// Increment when the wire format changes in a backward-incompatible way.
	ProtocolVersion uint32 = 8

	// ClientName identifies this implementation.
	ClientName = coinparams.NameLower

	// ReleasesURL is the URL where users can download the latest release.
	//
	// FORKERS: Update this URL to point to YOUR project's releases page so
	// that out-of-date notifications direct users to the correct download.
	ReleasesURL = "https://github.com/bams-repo/go-chain/releases"
)

// String returns the semantic version string (e.g. "0.1.0").
func String() string {
	return fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
}

// UserAgent returns the BIP-style user agent (e.g. "/fairchain:0.1.0/").
func UserAgent() string {
	return fmt.Sprintf("%s%s/", coinparams.UserAgentPrefix, String())
}

// SemVer holds a parsed major.minor.patch triple.
type SemVer struct {
	Major, Minor, Patch int
}

// ParseSemVer parses a "major.minor.patch" string. Returns ok=false on failure.
func ParseSemVer(s string) (SemVer, bool) {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return SemVer{}, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return SemVer{}, false
	}
	return SemVer{Major: maj, Minor: min, Patch: pat}, true
}

// IsNewerThan returns true if v is strictly newer than other.
func (v SemVer) IsNewerThan(other SemVer) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch > other.Patch
}

func (v SemVer) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// ExtractVersionFromUserAgent extracts the semver from a BIP-style user agent
// string like "/fairchain:0.8.1/". Returns the version string and ok=true on
// success.
func ExtractVersionFromUserAgent(ua string) (string, bool) {
	ua = strings.TrimPrefix(ua, "/")
	ua = strings.TrimSuffix(ua, "/")
	idx := strings.LastIndex(ua, ":")
	if idx < 0 || idx >= len(ua)-1 {
		return "", false
	}
	return ua[idx+1:], true
}

// Current returns the running node's version as a SemVer.
func Current() SemVer {
	return SemVer{Major: Major, Minor: Minor, Patch: Patch}
}
