package output

import (
	"io"

	"github.com/shamith/mongodiff/pkg/diff"
)

// Renderer formats and writes a DiffResult to an output stream.
type Renderer interface {
	Render(w io.Writer, result *diff.DiffResult) error
}
