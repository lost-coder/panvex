package auth

import "strings"

// commonBreachedPasswords is a curated subset (~100 entries) of the most
// commonly breached / guessed passwords, drawn from public corpora such as
// the NCSC top-100 (2024) and the RockYou leak top entries. These passwords
// — regardless of whether they meet the configured length policy — are
// universally rejected on set / change paths because they are guaranteed
// to appear in online credential-stuffing dictionaries.
//
// Constraints:
//   - Entries MUST be lowercase. Comparison is case-folded at lookup.
//   - The list is intentionally small and embedded inline; the goal is to
//     stop trivially-cracked passwords, not to replicate HIBP. A larger
//     blocklist belongs in a separate operator-managed source.
//   - Existing logins are NOT impacted: the denylist is consulted only on
//     SetUserPassword / UpdateUser code paths, where a new password is
//     being chosen.
var commonBreachedPasswords = map[string]struct{}{
	// NCSC 2024 top-100 / RockYou top-100 overlap — most-used passwords.
	"123456":      {},
	"123456789":   {},
	"12345678":    {},
	"12345":       {},
	"1234567":     {},
	"1234567890":  {},
	"qwerty":      {},
	"qwerty123":   {},
	"qwertyuiop":  {},
	"password":    {},
	"password1":   {},
	"password123": {},
	"passw0rd":    {},
	"p@ssw0rd":    {},
	"p@ssword":    {},
	"admin":       {},
	"admin123":    {},
	"administrator": {},
	"root":        {},
	"toor":        {},
	"letmein":     {},
	"letmein123":  {},
	"welcome":     {},
	"welcome1":    {},
	"welcome123":  {},
	"abc123":      {},
	"abcd1234":    {},
	"a1b2c3d4":    {},
	"iloveyou":    {},
	"iloveyou1":   {},
	"princess":    {},
	"sunshine":    {},
	"shadow":      {},
	"superman":    {},
	"batman":      {},
	"pokemon":     {},
	"michael":     {},
	"jennifer":    {},
	"jordan":      {},
	"jessica":     {},
	"daniel":      {},
	"thomas":      {},
	"charlie":     {},
	"andrew":      {},
	"matthew":     {},
	"joshua":      {},
	"anthony":     {},
	"ashley":      {},
	"buster":      {},
	"hunter":      {},
	"trustno1":    {},
	"freedom":     {},
	"liverpool":   {},
	"arsenal":     {},
	"chelsea":     {},
	"football":    {},
	"baseball":    {},
	"basketball":  {},
	"soccer":      {},
	"hockey":      {},
	"master":      {},
	"dragon":      {},
	"monkey":      {},
	"monkey123":   {},
	"tigger":      {},
	"cookie":      {},
	"summer":      {},
	"winter":      {},
	"autumn":      {},
	"spring":      {},
	"computer":    {},
	"internet":    {},
	"qazwsx":      {},
	"qazwsxedc":   {},
	"asdfgh":      {},
	"asdfghjkl":   {},
	"zxcvbn":      {},
	"zxcvbnm":     {},
	"1q2w3e":      {},
	"1q2w3e4r":    {},
	"1q2w3e4r5t":  {},
	"qwe123":      {},
	"qwe12345":    {},
	"q1w2e3r4":    {},
	"q1w2e3r4t5":  {},
	"111111":      {},
	"1111111":     {},
	"11111111":    {},
	"000000":      {},
	"0000000":     {},
	"123123":      {},
	"123123123":   {},
	"121212":      {},
	"112233":      {},
	"654321":      {},
	"55555":       {},
	"555555":      {},
	"666666":      {},
	"777777":      {},
	"888888":      {},
	"999999":      {},
	"changeme":    {},
	"changeme123": {},
	"default":     {},
	"guest":       {},
	"test123":     {},
	"test1234":    {},
	"temp123":     {},
	"temppass":    {},
	"newpass":     {},
	"login123":    {},
	"secret":      {},
	"secret123":   {},
	"qwerty1":     {},
	"baseball1":   {},
	"football1":   {},
}

// validatePasswordAgainstDenylist reports ErrPasswordCommonlyBreached if the
// supplied password (case-folded) matches a known breached / common entry.
// Returns nil for any password not in the embedded list.
//
// Apply this on password set / change paths only — never on verify, since
// existing accounts may have a current password on the list and locking
// them out would be a self-DoS.
func validatePasswordAgainstDenylist(password string) error {
	if _, ok := commonBreachedPasswords[strings.ToLower(password)]; ok {
		return ErrPasswordCommonlyBreached
	}
	return nil
}
