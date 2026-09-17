package vchasno

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// The live service does not always match its own documentation: the
// download-documents endpoint is documented with `status` as an int and
// `ready` / `pending` as bools, and actually answers with `status: "ready"`
// and `ready: 1`. These two types accept either shape, so one field changing
// representation cannot turn a successful call into a decode failure.

// FlexString holds a value the API may send as a string or as a number.
type FlexString string

// UnmarshalJSON accepts a string, a number, a bool or null.
func (f *FlexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*f = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(string(data))
	return nil
}

// MarshalJSON always writes a string, so tool answers have one stable shape.
func (f FlexString) MarshalJSON() ([]byte, error) { return json.Marshal(string(f)) }

// String returns the value as text.
func (f FlexString) String() string { return string(f) }

// FlexBool holds a value the API may send as a bool, a number or a string.
type FlexBool bool

// UnmarshalJSON accepts true/false, 1/0, "1"/"0" and null.
func (f *FlexBool) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*f = false
		return nil
	}
	s := string(data)
	if data[0] == '"' {
		var unquoted string
		if err := json.Unmarshal(data, &unquoted); err != nil {
			return err
		}
		s = unquoted
	}
	switch s {
	case "true", "1", "yes":
		*f = true
		return nil
	case "false", "0", "no", "":
		*f = false
		return nil
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		*f = n != 0
		return nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	*f = FlexBool(b)
	return nil
}

// MarshalJSON always writes a bool.
func (f FlexBool) MarshalJSON() ([]byte, error) { return json.Marshal(bool(f)) }
