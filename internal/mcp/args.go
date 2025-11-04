package mcp

import "fmt"

// parseStringArg extracts a string argument from an MCP arguments map.
// Returns an error if the argument is required but missing or invalid.
func parseStringArg(argsMap map[string]interface{}, key string, required bool) (string, error) {
	val, ok := argsMap[key]
	if !ok {
		if required {
			return "", fmt.Errorf("%s parameter is required", key)
		}
		return "", nil
	}

	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}

	if required && str == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}

	return str, nil
}

// parseIntArg extracts an integer argument from an MCP arguments map.
// MCP sends numbers as float64, so this handles the conversion.
// Returns defaultVal if the argument is missing or invalid.
func parseIntArg(argsMap map[string]interface{}, key string, defaultVal int) int {
	val, ok := argsMap[key]
	if !ok {
		return defaultVal
	}

	// MCP sends numbers as float64
	if f, ok := val.(float64); ok {
		return int(f)
	}

	return defaultVal
}

// parseIntArgPtr extracts an optional integer argument as a pointer.
// Returns nil if the argument is missing, or a pointer to the int value if present.
// Useful for distinguishing between "not provided" and "provided as 0".
func parseIntArgPtr(argsMap map[string]interface{}, key string) *int {
	val, ok := argsMap[key]
	if !ok {
		return nil
	}

	// MCP sends numbers as float64
	if f, ok := val.(float64); ok {
		result := int(f)
		return &result
	}

	return nil
}

// parseBoolArg extracts a boolean argument from an MCP arguments map.
// Returns defaultVal if the argument is missing or invalid.
func parseBoolArg(argsMap map[string]interface{}, key string, defaultVal bool) bool {
	val, ok := argsMap[key]
	if !ok {
		return defaultVal
	}

	if b, ok := val.(bool); ok {
		return b
	}

	return defaultVal
}

// parseArrayArg extracts a string array argument from an MCP arguments map.
// Returns nil if the argument is missing, or an empty slice if present but empty.
// Filters out non-string elements.
func parseArrayArg(argsMap map[string]interface{}, key string) []string {
	val, ok := argsMap[key]
	if !ok {
		return nil
	}

	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if str, ok := item.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// parseClampedInt extracts an integer argument and clamps it to [min, max].
// Returns defaultVal if the argument is missing or invalid.
func parseClampedInt(argsMap map[string]interface{}, key string, defaultVal, min, max int) int {
	val := parseIntArg(argsMap, key, defaultVal)
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
