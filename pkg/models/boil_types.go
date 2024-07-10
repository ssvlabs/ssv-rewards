// Code generated by SQLBoiler 4.16.2 (https://github.com/volatiletech/sqlboiler). DO NOT EDIT.
// This file is meant to be re-generated in place and/or deleted at any time.

package models

import (
	"strconv"

	"github.com/friendsofgo/errors"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/strmangle"
)

// M type is for providing columns and column values to UpdateAll.
type M map[string]interface{}

// ErrSyncFail occurs during insert when the record could not be retrieved in
// order to populate default value information. This usually happens when LastInsertId
// fails or there was a primary key configuration that was not resolvable.
var ErrSyncFail = errors.New("models: failed to synchronize data after insert")

type insertCache struct {
	query        string
	retQuery     string
	valueMapping []uint64
	retMapping   []uint64
}

type updateCache struct {
	query        string
	valueMapping []uint64
}

func makeCacheKey(cols boil.Columns, nzDefaults []string) string {
	buf := strmangle.GetBuffer()

	buf.WriteString(strconv.Itoa(cols.Kind))
	for _, w := range cols.Cols {
		buf.WriteString(w)
	}

	if len(nzDefaults) != 0 {
		buf.WriteByte('.')
	}
	for _, nz := range nzDefaults {
		buf.WriteString(nz)
	}

	str := buf.String()
	strmangle.PutBuffer(buf)
	return str
}

type ProviderType string

// Enum values for ProviderType
const (
	ProviderTypeE2m       ProviderType = "e2m"
	ProviderTypeBeaconcha ProviderType = "beaconcha"
)

func AllProviderType() []ProviderType {
	return []ProviderType{
		ProviderTypeE2m,
		ProviderTypeBeaconcha,
	}
}

func (e ProviderType) IsValid() error {
	switch e {
	case ProviderTypeE2m, ProviderTypeBeaconcha:
		return nil
	default:
		return errors.New("enum is not valid")
	}
}

func (e ProviderType) String() string {
	return string(e)
}

func (e ProviderType) Ordinal() int {
	switch e {
	case ProviderTypeE2m:
		return 0
	case ProviderTypeBeaconcha:
		return 1

	default:
		panic(errors.New("enum is not valid"))
	}
}
