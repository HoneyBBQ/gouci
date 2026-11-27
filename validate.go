package gouci

import "fmt"

const (
	djbHashInit = uint32(5381)
	djbHashMask = uint32(0x7fffffff)
	anonIDMask  = uint32(0xffff)
)

// validatePackageName matches libuci package names: non-empty [A-Za-z0-9_-].
func validatePackageName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !isASCIIAlnum(c) && c != '_' && c != '-' {
			return false
		}
	}

	return true
}

// validateName matches libuci uci_validate_name: non-empty [A-Za-z0-9_].
func validateName(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !isASCIIAlnum(c) && c != '_' {
			return false
		}
	}

	return true
}

// validateType matches libuci uci_validate_type: non-empty printable ASCII 33..126.
func validateType(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c < 33 || c > 126 {
			return false
		}
	}

	return true
}

// validateText matches libuci uci_validate_text: rejects control bytes < 32
// except '\t', '\n', and '\r'.
func validateText(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c < 32 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}

	return true
}

func isASCIIAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// djbHash reproduces libuci's djbhash from list.c for anonymous section IDs.
func djbHash(s string) uint32 {
	h := djbHashInit
	for i := range len(s) {
		h = ((h << 5) + h) + uint32(s[i])
	}

	return h & djbHashMask
}

// anonSectionID reproduces libuci's uci_fixup_section format ("cfg%02x%04x").
func anonSectionID(nSection int, typ string) string {
	return fmt.Sprintf("cfg%02x%04x", nSection, djbHash(typ)&anonIDMask)
}
