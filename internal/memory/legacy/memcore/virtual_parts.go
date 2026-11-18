// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package memcore

// VirtualPart represents an embedded code file, patch, or command output extracted from a raw turn
// and persisted in the database part store as a virtual evidence file.
type VirtualPart struct {
	Kind    string
	SubKind string
	URI     string
	Locator string
	Text    string
	Tool    string
}

const minVirtualPartTokens = TokenTier128
