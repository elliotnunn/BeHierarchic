package internpath

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestPackInt(t *testing.T) {
	numbers := []uint64{
		0, 1, 99, 127, 128, 129, 16383, 33,
	}
	for i := range 35 {
		numbers = append(numbers, 1<<i-1, 1<<i, 1<<i+1)
	}

	for _, want := range numbers {
		t.Run(fmt.Sprintf("%#x", want), func(t *testing.T) {
			saveBump := bump
			putg(want)
			repr := hex.EncodeToString(array[saveBump:bump])
			reprLen := bump - saveBump
			bump = saveBump
			remain, got := get[uint64](array[bump:])
			consumeLen := uint32(len(array[bump:]) - len(remain))

			if got != want {
				t.Errorf("number mismatch, wanted %#x got %#x, repr %s", want, got, repr)
			}
			if consumeLen != reprLen {
				t.Errorf("byte consumption mismatch, wrote out %d but read in %d, repr %s", reprLen, consumeLen, repr)
			}
		})
	}

}
