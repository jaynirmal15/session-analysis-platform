package livekit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// protoInt64 decodes a 64-bit integer that may arrive as a JSON number or a
// JSON string.
//
// This is not defensive coding, it is the wire format. LiveKit serialises its
// protobuf messages with the canonical protobuf JSON mapping, which encodes
// int64 and uint64 as *strings* — the JSON number type cannot represent the
// full 64-bit range without precision loss, so the spec sidesteps it.
//
// Found the only way it could be found: by pointing the receiver at a real
// LiveKit and watching every delivery fail to decode. The unit tests all passed
// beforehand, because the fixtures were written from the documentation's
// example shapes rather than from a captured delivery. Both forms are accepted
// here because the mapping is not uniformly applied across LiveKit versions and
// fields, and being wrong in this direction costs nothing.
type protoInt64 int64

func (v *protoInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*v = 0
		return nil
	}
	// Strip surrounding quotes if present.
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
		if len(b) == 0 {
			*v = 0
			return nil
		}
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		// Tolerate a float-formatted integer, which some encoders emit.
		var f float64
		if ferr := json.Unmarshal(b, &f); ferr == nil {
			*v = protoInt64(f)
			return nil
		}
		return fmt.Errorf("livekit: decode int64 %q: %w", b, err)
	}
	*v = protoInt64(n)
	return nil
}

func (v protoInt64) int64() int64 { return int64(v) }
