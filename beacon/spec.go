package beacon

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Spec struct {
	GenesisTime    time.Time
	SlotsPerEpoch  phase0.Slot
	SlotDuration   time.Duration
	FarFutureEpoch phase0.Epoch
}

func (s *Spec) FirstSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch) * s.SlotsPerEpoch
}

func (s *Spec) LastSlot(epoch phase0.Epoch) phase0.Slot {
	return s.FirstSlot(epoch+1) - 1
}

func (s *Spec) EpochAt(slot phase0.Slot) phase0.Epoch {
	return phase0.Epoch(slot / s.SlotsPerEpoch)
}

func (s *Spec) SlotAt(t time.Time) phase0.Slot {
	return phase0.Slot(t.Sub(s.GenesisTime) / s.SlotDuration)
}

func (s *Spec) TimeAt(slot phase0.Slot) time.Time {
	return s.GenesisTime.Add(time.Duration(slot) * s.SlotDuration)
}
