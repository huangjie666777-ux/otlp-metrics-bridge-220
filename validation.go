package main

import "fmt"

type invalidInputError struct {
	message string
}

func (e invalidInputError) Error() string { return e.message }

func errInvalid(format string, args ...any) error {
	return invalidInputError{message: fmt.Sprintf(format, args...)}
}

func isValidMetricName(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		if char == '_' || char == ':' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			continue
		}
		if i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

func isValidLabelName(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		if char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			continue
		}
		if i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}
