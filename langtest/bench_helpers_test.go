package langtest

// sink prevents the compiler from optimizing away a benchmark's computed
// result. Shared by every *_bench_test.go file in this package.
var sink any
