# cmd/lth — Test Scenarios

No unit tests for cmd/lth (thin CLI layer). CLI correctness is verified by:
- `go build ./cmd/lth` (build verification)
- `./lth --help` (help text renders without panic)
- Integration: the binary is tested through the pkg/lth layer tests
