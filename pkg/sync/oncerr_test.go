package sync

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnceErr(t *testing.T) {
	t.Parallel()

	called := 0
	fn := func() (int, error) {
		called++
		return 42, nil
	}

	memoizedFn := OnceErr(fn)

	value, err := memoizedFn()
	require.NoError(t, err)
	require.Equal(t, 42, value)
	require.Equal(t, 1, called)

	value, err = memoizedFn()
	require.NoError(t, err)
	require.Equal(t, 42, value)
	require.Equal(t, 1, called) // Didn't have to call the inner fn
}

func TestOnceErr_Error(t *testing.T) {
	t.Parallel()

	called := 0
	fn := func() (int, error) {
		called++
		return 1337, errors.New("test error")
	}

	memoizedFn := OnceErr(fn)

	value, err := memoizedFn()
	require.Error(t, err)
	require.Equal(t, 1337, value)
	require.Equal(t, 1, called)

	value, err = memoizedFn()
	require.Error(t, err)
	require.Equal(t, 1337, value)
	require.Equal(t, 1, called) // Didn't have to call the inner fn
}
