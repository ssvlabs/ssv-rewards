package precise

import (
	"encoding/json"
	"math/big"
)

const (
	precision = 256
	decimals  = 18
)

type ETH big.Float

func NewETH(f *big.Float) *ETH {
	if f == nil {
		f = new(big.Float)
	} else {
		f = new(big.Float).Copy(f)
	}
	return (*ETH)(f.SetPrec(precision))
}

func NewETH64(f float64) *ETH {
	return NewETH(big.NewFloat(f))
}

func ParseETH(s string) (*ETH, error) {
	f, _, err := new(big.Float).SetPrec(precision).Parse(s, 10)
	if err != nil {
		return nil, err
	}
	return NewETH(f), nil
}

func (e *ETH) Float() *big.Float {
	return (*big.Float)(e)
}

func (e *ETH) Mul(a, b *ETH) *ETH {
	e.Float().Mul((*big.Float)(a), (*big.Float)(b))
	return e
}

func (e *ETH) Quo(a, b *ETH) *ETH {
	e.Float().Quo((*big.Float)(a), (*big.Float)(b))
	return e
}

func (e *ETH) Add(a, b *ETH) *ETH {
	e.Float().Add((*big.Float)(a), (*big.Float)(b))
	return e
}

func (e *ETH) Sub(a, b *ETH) *ETH {
	e.Float().Sub((*big.Float)(a), (*big.Float)(b))
	return e
}

func (e *ETH) String() string {
	return e.Float().Text('f', decimals)
}

func (e *ETH) MarshalText() ([]byte, error) {
	return []byte(e.String()), nil
}

func (e *ETH) UnmarshalText(data []byte) error {
	v, _, err := new(big.Float).SetPrec(precision).Parse(string(data), 10)
	if err != nil {
		return err
	}
	*e = *NewETH(v)
	return nil
}

func (e *ETH) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *ETH) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return e.UnmarshalText([]byte(v))
}

func (e *ETH) Wei() *big.Int {
	wei := new(big.Int)
	copy := new(big.Float).Copy(e.Float())
	copy.Mul(copy, big.NewFloat(1e18))
	copy.Int(wei)
	return wei
}

func (e *ETH) SetWei(wei *big.Int) *ETH {
	e.Float().SetInt(wei)
	e.Float().Quo(e.Float(), big.NewFloat(1e18))
	return e
}

func (e *ETH) Gwei() *big.Int {
	gwei := new(big.Int)
	copy := new(big.Float).Copy(e.Float())
	copy.Mul(copy, big.NewFloat(1e9))
	copy.Int(gwei)
	return gwei
}
