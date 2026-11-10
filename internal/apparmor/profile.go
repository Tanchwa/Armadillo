/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package apparmor turns profile text from git into the text that is loaded
// on a node. Profile text is untrusted input (docs/design.md section 7): the
// name and the mode are always set here, never taken from the text.
package apparmor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Mode is the AppArmor mode a profile is loaded in.
type Mode string

const (
	// Complain logs what the profile would deny and blocks nothing.
	Complain Mode = "Complain"
	// Enforce denies what the profile does not allow.
	Enforce Mode = "Enforce"
)

// MaxProfileBytes is the largest profile text that is accepted.
const MaxProfileBytes = 256 * 1024

// ProfileName is the name a ManagedProfile is loaded under on every node, and
// the name pods reference. Namespaces cannot contain dots, so the name cannot
// collide with another namespace's profile or with a profile of the host.
func ProfileName(namespace, name string) string {
	return "armadillo." + namespace + "." + name
}

// Hash is the content hash of profile text, as recorded in
// ManagedProfile.status.profile.hash and ProfileNodeStatus.status.loadedHash.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var (
	// include <abstractions/base>, #include <tunables/global>
	includeLine = regexp.MustCompile(`^#?\s*include\b`)
	// Only the abstractions and tunables that ship with the parser. Anything
	// else, like a quoted path, makes the parser read a host file as root.
	allowedInclude = regexp.MustCompile(`^#?\s*include\s+<((?:abstractions|tunables)/[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)*)>$`)
	abiLine        = regexp.MustCompile(`^abi\s+<abi/[A-Za-z0-9_.-]+>,$`)
	// profile NAME [flags=(...)] {   or   NAME [flags=(...)] {
	headerLine = regexp.MustCompile(`^(?:profile\s+)?("[^"]+"|[^\s"{]+)(\s+flags=\([^)]*\))?\s*\{$`)
)

// checkBraces rejects a rule line that could close the profile or open another
// block. The parser reads tokens, not lines, so "} profile x /bin/sh { ..."
// on one line would otherwise smuggle in a second profile. Braces are only
// allowed balanced inside one word, as in "/sys/{a,b}/**" or "@{PROC}".
func checkBraces(line string) error {
	for _, word := range strings.Fields(line) {
		if strings.HasPrefix(word, "{") || strings.HasPrefix(word, "}") {
			return errors.New("a rule must not contain a free-standing brace")
		}
		depth := 0
		for _, c := range word {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth < 0 {
				break
			}
		}
		if depth != 0 {
			return errors.New("braces in a rule must be balanced within one word")
		}
	}
	return nil
}

// Render validates profile text and returns the text to load: the same rules
// under the given name, in the given mode.
//
// It accepts one profile per text, with no attachment, no nested profile or
// hat, and includes only of the parser's own abstractions and tunables.
func Render(text, name string, mode Mode) (string, error) {
	if len(text) > MaxProfileBytes {
		return "", fmt.Errorf("profile is %d bytes, the limit is %d", len(text), MaxProfileBytes)
	}
	if mode != Complain && mode != Enforce {
		return "", fmt.Errorf("unknown mode %q", mode)
	}

	var preamble, body []string
	const (
		beforeProfile = iota
		inProfile
		afterProfile
	)
	state := beforeProfile
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		lineErr := func(msg string) error { return fmt.Errorf("line %d: %s", i+1, msg) }

		if includeLine.MatchString(line) {
			if !allowedInclude.MatchString(line) {
				return "", lineErr("only includes of <abstractions/...> and <tunables/...> are allowed")
			}
			if strings.Contains(line, "..") {
				return "", lineErr("include path must not contain \"..\"")
			}
		} else if line == "" || strings.HasPrefix(line, "#") {
			// Comments are dropped: they are not policy.
			continue
		}

		switch state {
		case beforeProfile:
			switch {
			case includeLine.MatchString(line) || abiLine.MatchString(line):
				preamble = append(preamble, line)
			case headerLine.MatchString(line):
				state = inProfile
			case strings.HasSuffix(line, "{"):
				return "", lineErr("profile header must be \"profile NAME [flags=(...)] {\" with no attachment")
			default:
				return "", lineErr("only includes and an abi line may come before the profile")
			}
		case inProfile:
			switch {
			case line == "}":
				state = afterProfile
			case strings.HasSuffix(line, "{"):
				return "", lineErr("nested profiles and hats are not allowed")
			default:
				if err := checkBraces(line); err != nil {
					return "", lineErr(err.Error())
				}
				body = append(body, "  "+line)
			}
		case afterProfile:
			return "", lineErr("only one profile per file is allowed")
		}
	}
	switch state {
	case beforeProfile:
		return "", errors.New("no profile found")
	case inProfile:
		return "", errors.New("profile is not closed")
	}

	// attach_disconnected and mediate_deleted are what container runtimes need;
	// every other flag in the text, the mode above all, is discarded.
	flags := "attach_disconnected,mediate_deleted"
	if mode == Complain {
		flags += ",complain"
	}
	var out strings.Builder
	for _, l := range preamble {
		out.WriteString(l + "\n")
	}
	if len(preamble) > 0 {
		out.WriteString("\n")
	}
	fmt.Fprintf(&out, "profile %s flags=(%s) {\n", name, flags)
	for _, l := range body {
		out.WriteString(l + "\n")
	}
	out.WriteString("}\n")
	return out.String(), nil
}
