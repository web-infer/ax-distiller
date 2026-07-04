package structure

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestByteslicePool(t *testing.T) {
	b1 := getByteslice()
	b2 := getByteslice()
	b1 = append(b1, []byte("this")...)
	b2 = append(b2, []byte("that")...)
	require.Equal(t, b1, []byte("this"))
	require.Equal(t, b2, []byte("that"))
	putByteslice(b1)
	require.Equal(t, b2, []byte("that"))
	putByteslice(b2)

	b3 := getByteslice()
	b3 = append(b3, []byte("t")...)
	require.Equal(t, b3, []byte("t"))
	putByteslice(b3)
}
