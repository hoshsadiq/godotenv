package godotenv

// decodeEscape decodes the escape body that follows a backslash at s[j].
func decodeEscape(s []byte, j int) ([]byte, int, error) {
	switch s[j] {
	case 'a':
		return []byte{0x07}, 1, nil
	case 'b':
		return []byte{0x08}, 1, nil
	case 'e', 'E':
		return []byte{0x1b}, 1, nil
	case 'f':
		return []byte{0x0c}, 1, nil
	case 'n':
		return []byte{0x0a}, 1, nil
	case 'r':
		return []byte{0x0d}, 1, nil
	case 't':
		return []byte{0x09}, 1, nil
	case 'v':
		return []byte{0x0b}, 1, nil
	case '\\', '\'', '"', '?':
		return []byte{s[j]}, 1, nil
	case 'x':
		return decodeHexEscape(s, j, 2, false)
	case 'u':
		return decodeHexEscape(s, j, 4, true)
	case 'U':
		return decodeHexEscape(s, j, 8, true)
	}

	if s[j] >= '0' && s[j] <= '7' {
		n, k := 0, 0
		for k < 3 && j+k < len(s) && s[j+k] >= '0' && s[j+k] <= '7' {
			n = n*8 + int(s[j+k]-'0')
			k++
		}
		return []byte{byte(n)}, k, nil
	}

	return []byte{'\\', s[j]}, 1, nil
}

func decodeHexEscape(s []byte, j, max int, asRune bool) ([]byte, int, error) {
	n, k := 0, 0
	for k < max && j+1+k < len(s) && isHex(s[j+1+k]) {
		n = n*16 + hexVal(s[j+1+k])
		k++
	}
	if k == 0 {
		return []byte{'\\', s[j]}, 1, nil
	}
	if asRune {
		return []byte(string(rune(n))), 1 + k, nil
	}
	return []byte{byte(n)}, 1 + k, nil
}

func decodeUnicodeEscape(s []byte, j int) ([]byte, int) {
	n, k := 0, 0
	for k < 4 && j+1+k < len(s) && isHex(s[j+1+k]) {
		n = n*16 + hexVal(s[j+1+k])
		k++
	}
	if k == 0 {
		return []byte{'u'}, 0
	}
	return []byte(string(rune(n))), k
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
