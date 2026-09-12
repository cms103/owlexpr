package vm

import "github.com/cespare/xxhash/v2"

// HashName is the one hash function every part of the prehashing scheme
// agrees on: the compiler calls it once per identifier/member name to
// build a HashedName instruction Arg, RegisterBuiltin/RegisterNamespacedBuiltin
// call it (via PrehashedMap.Put) when a name is registered, and the two
// only ever agree on a bucket because both go through this same function.
// xxhash.Sum64String is a pure function of the string's bytes alone (unlike
// Go's built-in map hash, which is seeded per-map-instance) - that's what
// lets a hash computed once at compile time stay valid forever after,
// against any PrehashedMap, rather than just the one map it happened to be
// computed against.
func HashName(name string) uint64 {
	return xxhash.Sum64String(name)
}

// HashedName is the Arg an OpLoad or (unresolved) OpAccess/OpOptAccess
// instruction carries in place of a plain name string: the compiler
// computes Hash once, via HashName, when it emits the instruction, so the
// VM never has to hash Name itself no matter how many times that exact
// instruction executes (e.g. once per element in a map()/filter() lambda
// body, or once per row of a repeatedly-run sheet formula). Name is kept
// alongside Hash rather than discarded - it's still needed for the
// PrehashedMap bucket's own key comparison (a 64-bit hash collision, while
// astronomically unlikely, is not provably impossible) and for error
// messages.
//
// A plain string is still accepted everywhere a HashedName is (see
// asHashedName) - hand-built instructions (tests, or an embedder
// constructing []Instruction directly rather than through Compile) don't
// need to pre-hash anything; they just don't get the benefit of doing so.
type HashedName struct {
	Name string
	Hash uint64
}

// asHashedName normalizes an OpLoad/OpAccess/OpOptAccess instruction's Arg
// to a HashedName, computing the hash on the spot for the plain-string
// form. This keeps every hand-built Instruction (across this package's own
// tests, and any embedder building instructions directly rather than via
// Compile) working unchanged - only compiler.go's own emission needs to
// know HashedName exists at all to get the benefit of not re-hashing on
// every execution.
func asHashedName(arg any) HashedName {
	if hn, ok := arg.(HashedName); ok {
		return hn
	}
	name := arg.(string)
	return HashedName{Name: name, Hash: HashName(name)}
}

// entry is one PrehashedMap bucket slot: the original key alongside its
// value, kept so a hash collision (two different keys sharing one bucket)
// can still be told apart by an exact string comparison.
type entry[T any] struct {
	Key   string
	Value T
}

// PrehashedMap is a string-keyed map addressed by a precomputed hash (see
// HashName/HashedName) instead of a key that still needs hashing on every
// access. It exists for exactly the maps whose key set is fixed well
// before most of their reads happen - Machine.builtins and
// Machine.namespaces (both populated once, up front, then read on every
// matching OpLoad/OpAccess for the rest of the Machine's life) - where a
// compile-time hash computed once can be reused for the maps' entire
// lifetime, unlike Go's own map[string]T, whose hash seed is random per
// map instance and so can never be precomputed by anything outside it.
//
// It is not a drop-in replacement for map[string]T in general: Put/Get
// still take the string key (for the collision-comparison entry.Key check
// and for error messages), so a caller that doesn't already have both the
// name and its hash handy gets no benefit from using this over a plain
// map.
type PrehashedMap[T any] map[uint64][]entry[T]

// Put inserts or updates key's value, hashing it via HashName, and returns
// that hash so a caller that just registered a name (RegisterBuiltin,
// RegisterNamespacedBuiltin) can keep it around instead of recomputing it.
func (m PrehashedMap[T]) Put(key string, val T) uint64 {
	hash := HashName(key)

	bucket := m[hash]
	for i, e := range bucket {
		if e.Key == key {
			bucket[i].Value = val // Update
			return hash
		}
	}
	m[hash] = append(bucket, entry[T]{Key: key, Value: val}) // Insert
	return hash
}

// Get looks up key by its already-known hash - the whole point of calling
// this over a plain map[string]T being that hash was computed once, at
// compile time, rather than freshly on this call.
func (m PrehashedMap[T]) Get(hash uint64, key string) (T, bool) {
	var emptyT T
	bucket := m[hash]
	// Fast common path - only one value in this bucket
	if len(bucket) == 1 {
		return bucket[0].Value, true
	}

	for _, e := range m[hash] {
		if e.Key == key {
			return e.Value, true
		}
	}
	return emptyT, false
}
