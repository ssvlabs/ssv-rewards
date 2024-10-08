package rewards

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
)

type ExecutionAddress bellatrix.ExecutionAddress

func (e ExecutionAddress) String() string {
	b, err := e.MarshalText()
	if err != nil {
		return ""
	}
	return string(b)
}

func (e ExecutionAddress) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(e[:])), nil
}

func (e *ExecutionAddress) UnmarshalText(data []byte) error {
	if len(data) != 2+2*len(e) {
		return fmt.Errorf("invalid length, want %d bytes", 2+2*len(e))
	}
	if data[0] != '0' || data[1] != 'x' {
		return fmt.Errorf("invalid prefix, want 0x")
	}
	b, err := hex.DecodeString(string(data[2:]))
	if err != nil {
		return err
	}
	copy(e[:], b)
	return nil
}

func (e ExecutionAddress) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *ExecutionAddress) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return e.UnmarshalText([]byte(s))
}
