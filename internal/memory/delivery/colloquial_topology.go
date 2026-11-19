// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

// SubphraseNodeInput represents a referent (or slot-occupying proposition) node candidate
// for Hasse multi-level sub-phrase/entity hub construction.
type SubphraseNodeInput struct {
	ID         uint64
	Label      string
	Stems      []string
	FieldCount int
	IsPure     bool
	IsPoss     bool
	IsSlotProp bool // true when kind=='assertion' occupying a subject_id/object_id slot (acts only as ChildID hub, never ParentID)
}

// SubphraseLinkOutput represents a directed Hasse edge (ChildID --subtype_of--> ParentID) with its formal evidence tag.
type SubphraseLinkOutput struct {
	ChildID  uint64
	ParentID uint64
	Evidence string // "stem-suffix", "stem-prefix", "stem-subseq", or "stem-entity"
}
