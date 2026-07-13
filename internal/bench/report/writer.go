// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package report

import "os"

type Writer struct {
	f *os.File
}
