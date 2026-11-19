// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

import ()

func hasToken(toks []string, want string) bool {
	for _, tk := range toks {
		if tk == want {
			return true
		}
	}
	return false
}
