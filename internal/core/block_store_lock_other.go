// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

//go:build !darwin && !linux

package core

import "os"

func lockBlockStoreHandle(_ *os.File) error { return nil }

func unlockBlockStoreHandle(_ *os.File) error { return nil }
