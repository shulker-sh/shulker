//go:build !unix

package out

import "os"

func queryBackground(*os.File) ([3]float64, bool) { return [3]float64{}, false }
