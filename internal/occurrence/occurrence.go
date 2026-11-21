// Package occurrence names a chore's or routine's occurrence on one household day.
package occurrence

import (
	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
)

// namespace namespaces occurrence IDs.
var namespace = uuid.MustParse("6f1c2a52-6b0e-4c1e-9d63-3d1f0f7b2a10")

// ID is the occurrence of a definition on a household day: a UUIDv5 of the two, so the plan a
// read computes, the row a command creates and the row the importer writes agree.
func ID(definition uuid.UUID, d day.Date) uuid.UUID {
	return uuid.NewSHA1(namespace, append(definition[:], d.String()...))
}
