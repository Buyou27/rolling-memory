// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package plain

import "github.com/rocareer/rolling-memory/sdk/go/public/closedset"

func lookupFirstClosedSetDictTag(low string) string {
	return closedset.LookupFirstTag(low)
}
