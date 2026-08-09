package styles_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var colourLiteral = regexp.MustCompile(`lg\.Color\(|lipgloss\.Color\(`)

const paletteHome = "theme.go"

func TestNoRawColoursOutsideTheme(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../")
	require.NoError(t, err, "locate the tui tree")

	var offenders []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || filepath.Base(path) == paletteHome {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if colourLiteral.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"colours belong in styles.Theme, not in views - add a token and use it:\n%s",
		strings.Join(offenders, "\n"))
}
