// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package version

import (
	"fmt"

	"github.com/bams-repo/fairchain/internal/coinparams"
)

const (
	// Major is the major version component (breaking protocol changes).
	Major = 0

	// Minor is the minor version component (new features, backward-compatible).
	Minor = 8

	// Patch is the patch version component (bug fixes).
	Patch = 0

	// ProtocolVersion is the peer-to-peer wire protocol version.
	// Increment when the wire format changes in a backward-incompatible way.
	ProtocolVersion uint32 = 4

	// ClientName identifies this implementation.
	ClientName = coinparams.NameLower
)

// String returns the semantic version string (e.g. "0.1.0").
func String() string {
	return fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
}

// UserAgent returns the BIP-style user agent (e.g. "/fairchain:0.1.0/").
func UserAgent() string {
	return fmt.Sprintf("%s%s/", coinparams.UserAgentPrefix, String())
}
