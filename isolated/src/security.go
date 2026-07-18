package src

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"regexp"
)

// packageNameRe matches safe package/container identifiers: letters, digits,
// dot, underscore, plus and hyphen, with an optional leading '@' (used by
// package-manager *group* identifiers, e.g. dnf's "@kde-desktop-environment").
// '@' has no special meaning to /bin/sh when it appears in an ordinary
// argument like this, so allowing it doesn't weaken the shell-injection
// protection this function exists for.
var packageNameRe = regexp.MustCompile(`^@?[a-zA-Z0-9][a-zA-Z0-9._+-]{0,127}$`)

// ValidatePackageName rejects anything that is not a plain, shell-safe
// identifier. It is used on every user-supplied package name *and* on every
// package name coming from the remote repository JSON before it is ever
// concatenated into a command string that gets executed inside a container.
func ValidatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("package name cannot be empty")
	}
	if !packageNameRe.MatchString(name) {
		return fmt.Errorf("package name %q contains characters that are not allowed (only letters, digits, '.', '_', '+', '-')", name)
	}
	return nil
}

// ValidatePackageNames validates a whole slice (e.g. dependency lists).
func ValidatePackageNames(names []string) error {
	for _, n := range names {
		if err := ValidatePackageName(n); err != nil {
			return err
		}
	}
	return nil
}

// SHA256Hex returns the lowercase hex-encoded SHA-256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// VerifyChecksum performs a constant-time comparison between the computed
// SHA-256 of data and an expected hex digest (case-insensitive, whitespace
// tolerant — checksum files commonly look like "<hex>  filename").
func VerifyChecksum(data []byte, expectedHex string) bool {
	expectedHex = firstToken(expectedHex)
	got := SHA256Hex(data)
	return subtle.ConstantTimeCompare([]byte(got), []byte(expectedHex)) == 1
}

// firstToken returns the first whitespace-separated token of s, lower-cased.
func firstToken(s string) string {
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start != -1 {
				return toLowerASCII(s[start:i])
			}
			continue
		}
		if start == -1 {
			start = i
		}
	}
	if start == -1 {
		return ""
	}
	return toLowerASCII(s[start:])
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
