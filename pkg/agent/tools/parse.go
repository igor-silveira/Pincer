package tools

import (
	"encoding/json"
	"fmt"
)

func parseInput[T any](input json.RawMessage, toolName string) (T, error) {
	var params T
	if err := json.Unmarshal(input, &params); err != nil {
		return params, fmt.Errorf("%s: invalid input: %w", toolName, err)
	}
	return params, nil
}
