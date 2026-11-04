package psprcs

import (
	"errors"
	"fmt"
	"strings"
)

// MaxGenericProcessNameLen is the maximum allowed length for generic/custom process types (8 characters).
// PeopleTools enforces this limit in PRCSDEFN.PRCSNAME.SearchSave for all process types
// where Generic Process Type is not Application Engine (GENPRCSTYPE != 5).
const MaxGenericProcessNameLen = 8

// MaxAEProcessNameLen is the maximum allowed length for Application Engine programs (12 characters).
const MaxAEProcessNameLen = 12

// ValidateProcessName validates that a process name conforms to PeopleSoft PeopleTools constraints:
// - Non-empty
// - At most 8 characters long for generic/custom process types (or maxLen if custom specified)
// - Uppercase only
// - Alphanumeric characters and underscores only
func ValidateProcessName(name string, maxLen ...int) error {
	limit := MaxGenericProcessNameLen
	if len(maxLen) > 0 && maxLen[0] > 0 {
		limit = maxLen[0]
	}

	if len(name) == 0 {
		return errors.New("process name cannot be empty")
	}
	if len(name) > limit {
		return fmt.Errorf("process name %q exceeds maximum length of %d characters (got %d)", name, limit, len(name))
	}
	if name != strings.ToUpper(name) {
		return fmt.Errorf("process name %q must be uppercase", name)
	}
	for _, r := range name {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return fmt.Errorf("process name %q contains invalid character %q (only A-Z, 0-9, and _ allowed)", name, r)
		}
	}
	return nil
}
