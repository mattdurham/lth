// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import "os"

func main() {
	if err := Execute(); err != nil {
		os.Exit(1)
	}
}
