//go:build !unix

package out

import "os"

func queryBackground(_, _ *os.File) ([3]float64, bool) { return [3]float64{}, false }
