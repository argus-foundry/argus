package api

import (
	"strconv"
	"unicode/utf8"
)

// validBridgeUnicode checks syntactically valid JSON before the decoded events
// apply. encoding/json silently replaces invalid UTF-8 and unpaired surrogates.
func validBridgeUnicode(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if body[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		i += 4
		if code < 0xd800 || code > 0xdfff {
			continue
		}
		if code > 0xdbff || i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
			return false
		}
		low, _ := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
