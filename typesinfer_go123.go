//go:build go1.23
// +build go1.23

package gogen

import (
	"fmt"
	"go/types"
	_ "unsafe"
)

const (
	invalid   operandMode = iota // operand is invalid
	novalue                      // operand represents no value (result of a function call w/o result)
	builtin                      // operand is a built-in function
	typexpr                      // operand is a type
	constant_                    // operand is a constant; the operand's typ is a Basic type
	variable                     // operand is an addressable variable
	mapindex                     // operand is a map index expression (acts like a variable on lhs, commaok on rhs of an assignment)
	value                        // operand is a computed value
	nilvalue                     // operand is the nil value - only used by types2
	commaok                      // like value, but operand may be used in a comma,ok expression
	commaerr                     // like commaok, but second value is error, not boolean
	cgofunc                      // operand is a cgo function
)

type errorDesc struct {
	posn positioner
	msg  string
}

// An error_ represents a type-checking error.
// A new error_ is created with Checker.newError.
// To report an error_, call error_.report.
type error_ struct {
	check *types.Checker
	desc  []errorDesc
	code  int
	soft  bool
}

//go:linkname checker_infer123 go/types.(*Checker).infer
func checker_infer123(check *types.Checker, posn positioner, tparams []*types.TypeParam, targs []types.Type, params *Tuple, args []*operand, reverse bool, err *error_) (inferred []types.Type)

func checker_infer(check *types.Checker, posn positioner, tparams []*types.TypeParam, targs []types.Type, params *types.Tuple, args []*operand) (result []types.Type, err error) {
	const CannotInferTypeArgs = 138
	_err := &error_{check: check, code: CannotInferTypeArgs}
	result = checker_infer123(check, posn, tparams, targs, params, args, true, _err)
	if len(_err.desc) > 0 {
		err = fmt.Errorf("%s", _err.desc[0].msg)
	}
	return
}

func infer(pkg *Package, posn positioner, tparams []*types.TypeParam, targs []types.Type, params *types.Tuple, args []*operand) (result []types.Type, err error) {
	conf := &types.Config{
		Error: func(e error) {
			err = e
			if terr, ok := e.(types.Error); ok {
				err = fmt.Errorf("%s", terr.Msg)
			}
		},
	}
	checker := types.NewChecker(conf, pkg.Fset, pkg.Types, nil)
	result, err = checker_infer(checker, posn, tparams, targs, params, args)
	return
}
