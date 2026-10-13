package banner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultIsTheDockerAgentArt(t *testing.T) {
	t.Parallel()

	b := Default()
	assert.False(t, b.Animated())
	assert.Equal(t, Width, b.Width())
	assert.Equal(t, Height, b.Height())
	assert.Equal(t, strings.Join(Lines, "\n"), b.FrameAt(time.Hour))
}

func TestParseSingleFrame(t *testing.T) {
	t.Parallel()

	b, err := Parse("\n ab\nabcd\n\n")
	require.NoError(t, err)
	assert.False(t, b.Animated())
	assert.Equal(t, 4, b.Width())
	assert.Equal(t, 2, b.Height())
	assert.Equal(t, " ab \nabcd", b.FrameAt(0))
}

func TestParseFramesShareOneSize(t *testing.T) {
	t.Parallel()

	b, err := Parse("ab\ncd\n\f\nxyz\n\f\n\x1b[31mq\x1b[39m\n")
	require.NoError(t, err)
	assert.True(t, b.Animated())
	assert.Equal(t, 3, b.Width(), "color sequences take no width")
	assert.Equal(t, 2, b.Height())
	assert.Equal(t, "ab \ncd ", b.FrameAt(0))
	assert.Equal(t, "xyz\n   ", b.FrameAt(FrameDuration))
	assert.Equal(t, "\x1b[31mq\x1b[39m  \n   ", b.FrameAt(2*FrameDuration))
	assert.Equal(t, "ab \ncd ", b.FrameAt(3*FrameDuration), "the frames loop")
}

func TestParseRejectsEmptyArt(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "\n\n", "  \n\f\n\n"} {
		_, err := Parse(text)
		require.Error(t, err)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "banner.txt")
	require.NoError(t, os.WriteFile(path, []byte("one\r\n\f\r\ntwo\r\n"), 0o600))
	b, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "one", b.FrameAt(0))
	assert.Equal(t, "two", b.FrameAt(FrameDuration))

	_, err = Load(filepath.Join(t.TempDir(), "missing.txt"))
	require.Error(t, err)

	require.NoError(t, os.WriteFile(path, nil, 0o600))
	_, err = Load(path)
	require.ErrorContains(t, err, path)
}
