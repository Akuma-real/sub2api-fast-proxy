package proxy

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func forceTopLevelStringProperty(body []byte, key, value string) ([]byte, bool, error) {
	if key == "" {
		return body, false, fmt.Errorf("key cannot be empty")
	}

	start := firstNonSpace(body)
	if start < 0 {
		return body, false, fmt.Errorf("empty JSON body")
	}
	end := lastNonSpaceExclusive(body)
	if body[start] != '{' {
		return body, false, fmt.Errorf("JSON body must be an object")
	}

	i := start + 1
	hasFields := false
	for {
		i = skipSpaces(body, i, end)
		if i >= end {
			return body, false, fmt.Errorf("unterminated JSON object")
		}
		if body[i] == '}' {
			if i != end-1 {
				return body, false, fmt.Errorf("unexpected bytes after JSON object")
			}
			insertPos := trimSpaceBackward(body, start+1, i)
			field := quoteJSONField(key, value)
			if hasFields {
				field = append([]byte{','}, field...)
			}
			out := make([]byte, 0, len(body)+len(field))
			out = append(out, body[:insertPos]...)
			out = append(out, field...)
			out = append(out, body[insertPos:]...)
			return out, true, nil
		}

		if body[i] != '"' {
			return body, false, fmt.Errorf("expected object key at byte %d", i)
		}
		keyEnd, err := scanJSONString(body, i, end)
		if err != nil {
			return body, false, err
		}
		parsedKey, err := strconv.Unquote(string(body[i:keyEnd]))
		if err != nil {
			return body, false, fmt.Errorf("decode object key at byte %d: %w", i, err)
		}

		i = skipSpaces(body, keyEnd, end)
		if i >= end || body[i] != ':' {
			return body, false, fmt.Errorf("expected ':' after key %q", parsedKey)
		}

		valueStart := skipSpaces(body, i+1, end)
		valueEnd, err := skipJSONValue(body, valueStart, end)
		if err != nil {
			return body, false, fmt.Errorf("parse value for key %q: %w", parsedKey, err)
		}

		if parsedKey == key {
			quoted := quoteJSONString(value)
			out := make([]byte, 0, len(body)-valueEnd+valueStart+len(quoted))
			out = append(out, body[:valueStart]...)
			out = append(out, quoted...)
			out = append(out, body[valueEnd:]...)
			return out, true, nil
		}

		hasFields = true
		i = skipSpaces(body, valueEnd, end)
		if i >= end {
			return body, false, fmt.Errorf("unterminated JSON object")
		}
		switch body[i] {
		case ',':
			i++
		case '}':
			if i != end-1 {
				return body, false, fmt.Errorf("unexpected bytes after JSON object")
			}
			insertPos := trimSpaceBackward(body, start+1, i)
			field := append([]byte{','}, quoteJSONField(key, value)...)
			out := make([]byte, 0, len(body)+len(field))
			out = append(out, body[:insertPos]...)
			out = append(out, field...)
			out = append(out, body[insertPos:]...)
			return out, true, nil
		default:
			return body, false, fmt.Errorf("expected ',' or '}' after key %q", parsedKey)
		}
	}
}

func quoteJSONField(key, value string) []byte {
	out := quoteJSONString(key)
	out = append(out, ':')
	out = append(out, quoteJSONString(value)...)
	return out
}

func quoteJSONString(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func skipJSONValue(body []byte, i, limit int) (int, error) {
	i = skipSpaces(body, i, limit)
	if i >= limit {
		return i, fmt.Errorf("missing value")
	}

	switch body[i] {
	case '"':
		return scanJSONString(body, i, limit)
	case '{', '[':
		return skipCompositeJSONValue(body, i, limit)
	default:
		start := i
		for i < limit {
			switch body[i] {
			case ',', '}', ']':
				if i == start {
					return i, fmt.Errorf("missing scalar value")
				}
				return i, nil
			case ' ', '\n', '\r', '\t':
				if i == start {
					return i, fmt.Errorf("missing scalar value")
				}
				return i, nil
			default:
				i++
			}
		}
		if i == start {
			return i, fmt.Errorf("missing scalar value")
		}
		return i, nil
	}
}

func skipCompositeJSONValue(body []byte, i, limit int) (int, error) {
	stack := []byte{body[i]}
	i++
	for i < limit {
		switch body[i] {
		case '"':
			next, err := scanJSONString(body, i, limit)
			if err != nil {
				return i, err
			}
			i = next
		case '{', '[':
			stack = append(stack, body[i])
			i++
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return i, fmt.Errorf("unexpected '}' at byte %d", i)
			}
			stack = stack[:len(stack)-1]
			i++
			if len(stack) == 0 {
				return i, nil
			}
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return i, fmt.Errorf("unexpected ']' at byte %d", i)
			}
			stack = stack[:len(stack)-1]
			i++
			if len(stack) == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return i, fmt.Errorf("unterminated composite value")
}

func scanJSONString(body []byte, i, limit int) (int, error) {
	if i >= limit || body[i] != '"' {
		return i, fmt.Errorf("expected string at byte %d", i)
	}
	i++
	for i < limit {
		switch body[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1, nil
		default:
			i++
		}
	}
	return i, fmt.Errorf("unterminated string")
}

func firstNonSpace(body []byte) int {
	for i := range body {
		if !isJSONSpace(body[i]) {
			return i
		}
	}
	return -1
}

func lastNonSpaceExclusive(body []byte) int {
	for i := len(body) - 1; i >= 0; i-- {
		if !isJSONSpace(body[i]) {
			return i + 1
		}
	}
	return 0
}

func skipSpaces(body []byte, i, limit int) int {
	for i < limit && isJSONSpace(body[i]) {
		i++
	}
	return i
}

func trimSpaceBackward(body []byte, min, i int) int {
	for i > min && isJSONSpace(body[i-1]) {
		i--
	}
	return i
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}
