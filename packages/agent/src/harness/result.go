package harness

import (
	"encoding/json"
	"fmt"
)

type Result[TValue any, TError error] struct {
	OK    bool
	Value TValue
	Error TError
}

func Ok[TValue any, TError error](value TValue) Result[TValue, TError] {
	return Result[TValue, TError]{OK: true, Value: value}
}

func Err[TValue any, TError error](err TError) Result[TValue, TError] {
	return Result[TValue, TError]{Error: err}
}

func GetOrThrow[TValue any, TError error](result Result[TValue, TError]) (TValue, error) {
	if !result.OK {
		var zero TValue
		return zero, result.Error
	}
	return result.Value, nil
}

func GetOrZero[TValue any, TError error](result Result[TValue, TError]) TValue {
	if !result.OK {
		var zero TValue
		return zero
	}
	return result.Value
}

func GetOrUndefined[TValue any, TError error](result Result[TValue, TError]) *TValue {
	if !result.OK {
		return nil
	}
	return &result.Value
}

func ToError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	if text, ok := value.(string); ok {
		return fmt.Errorf("%s", text)
	}
	raw, err := json.Marshal(value)
	if err == nil {
		return fmt.Errorf("%s", string(raw))
	}
	return fmt.Errorf("%v", value)
}
