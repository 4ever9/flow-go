package runtime

import (
	"github.com/dapperlabs/flow-go/language/runtime/ast"
	"github.com/dapperlabs/flow-go/language/runtime/interpreter"
	"github.com/dapperlabs/flow-go/language/runtime/parser"
	"github.com/dapperlabs/flow-go/language/runtime/sema"
	"github.com/dapperlabs/flow-go/language/runtime/trampoline"
)

type REPL struct {
	checker  *sema.Checker
	inter    *interpreter.Interpreter
	onError  func(error)
	onResult func(interpreter.Value)
}

func NewREPL(onError func(error), onResult func(interpreter.Value)) (*REPL, error) {
	checker, err := sema.NewChecker(nil, REPLLocation{})
	if err != nil {
		return nil, err
	}

	inter, err := interpreter.NewInterpreter(checker)
	if err != nil {
		return nil, err
	}

	repl := &REPL{
		checker:  checker,
		inter:    inter,
		onError:  onError,
		onResult: onResult,
	}
	return repl, nil
}

func (r *REPL) handleCheckerError(code string) bool {
	err := r.checker.CheckerError()
	if err == nil {
		return true
	}
	if r.onError != nil {
		r.onError(err)
	}
	return false
}

func (r *REPL) execute(element ast.Element) {
	result := trampoline.Run(element.Accept(r.inter).(trampoline.Trampoline))
	expStatementRes, ok := result.(interpreter.ExpressionStatementResult)
	if !ok {
		return
	}
	if r.onResult == nil {
		return
	}
	r.onResult(expStatementRes.Value)
}

func (r *REPL) check(element ast.Element, code string) bool {
	element.Accept(r.checker)
	return !r.handleCheckerError(code)
}

func (r *REPL) Accept(code string) (inputIsComplete bool) {
	var result interface{}
	var err error
	result, inputIsComplete, err = parser.ParseReplInput(code)

	if !inputIsComplete {
		return
	}

	if err != nil {
		r.onError(err)
		return
	}

	r.checker.ResetErrors()

	switch typedResult := result.(type) {
	case *ast.Program:
		if !r.check(typedResult, code) {
			return
		}

		r.checker.Program = typedResult

		r.execute(typedResult)

	case []ast.Statement:
		r.checker.Program = nil

		for _, statement := range typedResult {
			if !r.check(statement, code) {
				return
			}

			r.execute(statement)
		}
	}

	return
}
