package session

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that serialises as a human-readable string.
//
// Go's default JSON encoding for a duration is an integer count of
// nanoseconds, which would make the session record show 1800000000000 where a
// reader wants 30m0s. Being able to `cat` state and understand it is a stated
// value of this project (principle 3), and this type is what buys it.
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// MarshalJSON writes the duration as a string such as "30m0s".
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts the string form, and also a bare number of
// nanoseconds so that a record written by an older or hand-edited file is not
// rejected outright.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		parsed, err := time.ParseDuration(asString)
		if err != nil {
			return fmt.Errorf("parsing duration %q: %w", asString, err)
		}
		*d = Duration(parsed)
		return nil
	}

	var asNanos int64
	if err := json.Unmarshal(data, &asNanos); err != nil {
		return fmt.Errorf("duration must be a string or a number, got %s", data)
	}
	*d = Duration(asNanos)
	return nil
}
