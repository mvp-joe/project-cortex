package mcputils

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// ArgumentGetter is an interface for getting arguments from a request
type ArgumentGetter interface {
	GetArguments() map[string]interface{}
}

// CoerceBindArguments binds MCP request arguments to a target struct with proper type coercion
// This handles cases where MCP clients (like Claude) send all parameters as strings, including
// JSON-encoded arrays and objects.
func CoerceBindArguments[T any](request ArgumentGetter, target *T) error {
	rawArgs := request.GetArguments()

	// Create a custom decode hook for JSON strings
	jsonStringHook := func(
		f reflect.Type,
		t reflect.Type,
		data interface{},
	) (interface{}, error) {
		// Only process strings
		if f.Kind() != reflect.String {
			return data, nil
		}

		raw := data.(string)

		// Skip empty strings
		if raw == "" {
			return data, nil
		}

		// Try to parse as JSON for slices, maps, and structs
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Map || t.Kind() == reflect.Struct {
			// Check if it looks like JSON
			trimmed := strings.TrimSpace(raw)
			if (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) ||
				(strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) {

				// For slices, we need to unmarshal into the correct type
				if t.Kind() == reflect.Slice {
					// Create a new slice of the target type
					slicePtr := reflect.New(t)
					if err := json.Unmarshal([]byte(raw), slicePtr.Interface()); err == nil {
						return slicePtr.Elem().Interface(), nil
					}
				} else {
					// For maps and structs, unmarshal generically
					var result interface{}
					if err := json.Unmarshal([]byte(raw), &result); err == nil {
						return result, nil
					}
				}
			}
		}

		// Try to parse as JSON for booleans
		if t.Kind() == reflect.Bool {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "true" || trimmed == "false" {
				var result bool
				if err := json.Unmarshal([]byte(raw), &result); err == nil {
					return result, nil
				}
			}
		}

		// Try to parse as JSON for numbers
		if t.Kind() >= reflect.Int && t.Kind() <= reflect.Float64 {
			var result json.Number
			if err := json.Unmarshal([]byte(raw), &result); err == nil {
				// Let mapstructure handle the number conversion
				return result, nil
			}
		}

		return data, nil
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			jsonStringHook,
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
		Result:  target,
		TagName: "json", // Use json tags for field mapping
	})
	if err != nil {
		return err
	}

	return decoder.Decode(rawArgs)
}
