package owlexpr

import (
	"github.com/cms103/owlexpr/vm"
)

// NewVM constructs a VM ready to evaluate compiled expressions. len/sum/
// min/max/map/filter/reduce are always available with no option needed;
// pass stdlib.StringBuiltins()/TimeBuiltins() (or your own VMOption, via
// vm.RegisterOperation/vm.RegisterBuiltIn/vm.RegisterTypeCoder) for
// anything beyond that.
//
// registerBuiltins (builtin_funcs.go) is injected here as the *first*
// VMOption, ahead of any the caller supplied, rather than being hardcoded
// into vm.UnconfiguredVM itself. That ordering is what lets
// vm.ClearOperations()/vm.DisableBuiltIns(), passed as later options, still
// override these defaults, while keeping the vm package itself unaware that
// builtins (a root-level, language-specific concept) exist at all.
func NewVM(opts ...vm.VMOption) (*vm.Machine, error) {
	all := make([]vm.VMOption, 0, len(opts)+1)
	all = append(all, registerBuiltins)
	all = append(all, opts...)
	return vm.UnconfiguredVM(all...)
}
