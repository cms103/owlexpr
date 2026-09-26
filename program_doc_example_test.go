package owlexpr_test

import (
	"fmt"

	"github.com/cms103/owlexpr"
)

func ExampleCompile_names() {
	program, _ := owlexpr.Compile(`let limit = max; attributes.Fee ?? round(value * rate) > limit`)
	fmt.Println(program.Names())
	// Output: [attributes max rate round value]
}
