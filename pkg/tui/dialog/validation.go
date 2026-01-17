package dialog

import (
	"net/mail"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// patternCache caches compiled regex patterns to avoid repeated compilation.
var patternCache sync.Map // map[string]*regexp.Regexp

// getCompiledPattern returns a compiled regex pattern, using cache when available.
func getCompiledPattern(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	patternCache.Store(pattern, compiled)
	return compiled, nil
}

// validateStringField validates a string value against field constraints.
func validateStringField(val string, field ElicitationField) bool {
	if field.MinLength > 0 && len(val) < field.MinLength {
		return false
	}
	if field.Pattern != "" {
		compiled, err := getCompiledPattern(field.Pattern)
		if err != nil || !compiled.MatchString(val) {
			return false
		}
	}
	return validateFormat(val, field.Format)
}

// validateNumberField validates a numeric value against field constraints.
func validateNumberField(val float64, field ElicitationField) bool {
	if field.HasMinimum && val < field.Minimum {
		return false
	}
	if field.HasMaximum && val > field.Maximum {
		return false
	}
	return true
}

// validateFormat validates a string value against a JSON Schema format.
func validateFormat(val, format string) bool {
	switch format {
	case "":
		return true
	case "email":
		_, err := mail.ParseAddress(val)
		return err == nil
	case "uri":
		u, err := url.Parse(val)
		return err == nil && u.Scheme != "" && u.Host != ""
	case "date":
		_, err := time.Parse("2006-01-02", val)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, val)
		return err == nil
	default:
		return true // Unknown format - be permissive
	}
}
