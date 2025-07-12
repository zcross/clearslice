package clearslice

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Doc is the documentation for the clearslice linter.
const Doc = `clearslice detects when slices of non-primitive types are resized to zero length without explicitly clearing elements.
This helps prevent unintended liveness of objects in the underlying array, which can delay garbage collection.
It recommends using slices.Delete to clear elements up to the full capacity when resetting the length to zero.
It now avoids false positives when clear() is called immediately before resizing to zero.`

var analyzer = &analysis.Analyzer{
	Name:     "clearslice",
	Doc:      Doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// NewAnalyzer creates the singleton instance of the clearslice analyzer.
func NewAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     analyzer.Name,
		Doc:      analyzer.Doc,
		Requires: analyzer.Requires,
		Run:      run,
	}
}

// run executes the clearslice linter.
func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// We need to inspect BlockStmts (and similar statement lists) to check for sequential statements.
	nodeFilter := []ast.Node{
		(*ast.BlockStmt)(nil),
		(*ast.CaseClause)(nil), // For switch statements
		(*ast.CommClause)(nil), // For select statements
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		var stmts []ast.Stmt
		switch node := n.(type) {
		case *ast.BlockStmt:
			stmts = node.List
		case *ast.CaseClause:
			stmts = node.Body
		case *ast.CommClause:
			stmts = node.Body
		default:
			return
		}

		for i, stmt := range stmts {
			assignStmt, ok := stmt.(*ast.AssignStmt)
			if !ok {
				continue
			}

			// Check for `foo = foo[:0]` or `myObj.sliceField = myObj.sliceField[:0]` patterns.
			if len(assignStmt.Lhs) != 1 || len(assignStmt.Rhs) != 1 {
				continue
			}

			// The LHS can be either an identifier (e.g., `x`) or a selector expression (e.g., `myObj.sliceField`).
			var lhsExpr ast.Expr
			var sliceName string // This will store "x" or "myObj.sliceField" as a string for reporting

			switch lhs := assignStmt.Lhs[0].(type) {
			case *ast.Ident:
				lhsExpr = lhs
				sliceName = lhs.Name
			case *ast.SelectorExpr:
				lhsExpr = lhs
				// Reconstruct the full selector expression name for reporting.
				// This is a simplified reconstruction; for complex cases, pass.Fset.Position(lhs.Pos()).String()
				// or a more robust AST printer might be needed.
				if xIdent, ok := lhs.X.(*ast.Ident); ok {
					sliceName = xIdent.Name + "." + lhs.Sel.Name
				} else {
					// If the selector's X is not an ident (e.g., a function call returning a struct),
					// we might not be able to easily get a string name, so skip for now.
					continue
				}
			default:
				continue // Not an identifier or selector, not interested.
			}

			rhsSliceExpr, ok := assignStmt.Rhs[0].(*ast.SliceExpr)
			if !ok {
				continue
			}

			// Ensure the right-hand side's sliced expression matches the left-hand side.
			// This requires comparing the AST nodes themselves, not just their string names.
			if !identicalExpr(lhsExpr, rhsSliceExpr.X) {
				continue
			}

			// Check if the high index of the slice expression is a literal "0".
			if rhsSliceExpr.High == nil {
				continue
			}
			highLit, ok := rhsSliceExpr.High.(*ast.BasicLit)
			if !ok || highLit.Value != "0" {
				continue
			}

			// Get the type of the LHS expression (the slice itself).
			sliceType := pass.TypesInfo.TypeOf(lhsExpr)
			if sliceType == nil {
				continue
			}

			slice, ok := sliceType.Underlying().(*types.Slice)
			if !ok {
				continue
			}

			elemType := slice.Elem()

			// Check if the element type is a reference type.
			if !isOrContainsReferenceTypes(elemType) {
				continue
			}

			if i > 0 { // Check if there's a previous statement
				prevStmt := stmts[i-1]
				if exprStmt, isExprStmt := prevStmt.(*ast.ExprStmt); isExprStmt {
					if callExpr, isCallExpr := exprStmt.X.(*ast.CallExpr); isCallExpr {
						if funIdent, isFunIdent := callExpr.Fun.(*ast.Ident); isFunIdent {
							// Check if the function is the built-in `clear`
							// The `clear` built-in has a nil Object but a *types.Builtin type.
							if funIdent.Name == "clear" {
								if builtin, isBuiltin := pass.TypesInfo.Uses[funIdent].(*types.Builtin); isBuiltin && builtin.Name() == "clear" {
									if len(callExpr.Args) == 1 {
										clearArg := callExpr.Args[0]
										// Check if the argument to clear() is the same slice expression
										if identicalExpr(lhsExpr, clearArg) {
											// Found a preceding clear() call for the same slice.
											// This is a false positive, so skip reporting for this assignment.
											continue // Continue to the next statement in the current block
										}
									}
								}
							}
						}
					}
				}
			}

			// If we reach here, it means no preceding clear() was found, so report the diagnostic.
			startPos := assignStmt.Pos()
			endPos := assignStmt.End()

			replacement := sliceName + " = slices.Delete(" + sliceName + ", 0, len(" + sliceName + "))"

			pass.Report(analysis.Diagnostic{
				Pos:     startPos,
				End:     endPos,
				Message: "slice " + sliceName + " of type " + elemType.String() + " is resized to zero length without clearing elements",
				SuggestedFixes: []analysis.SuggestedFix{
					{
						Message: "Replace with slices.Delete to clear elements before len adjustment.",
						TextEdits: []analysis.TextEdit{
							{
								Pos:     startPos,
								End:     endPos,
								NewText: []byte(replacement),
							},
						},
					},
				},
			})
		}
	})

	return nil, nil
}

// identicalExpr compares two ast.Expr nodes for structural equivalence.
// It handles identifiers and selector expressions for this linter's use case.
func identicalExpr(a, b ast.Expr) bool {
	switch a := a.(type) {
	case *ast.Ident:
		bIdent, ok := b.(*ast.Ident)
		return ok && a.Name == bIdent.Name
	case *ast.SelectorExpr:
		bSel, ok := b.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		return identicalExpr(a.X, bSel.X) && a.Sel.Name == bSel.Sel.Name
	default:
		return false
	}
}

// isOrContainsReferenceTypes checks if a given type is a reference type or a composite type that can contain references.
// It explicitly excludes basic (primitive) types.
func isOrContainsReferenceTypes(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool,
			types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr,
			types.Float32, types.Float64,
			types.Complex64, types.Complex128:
			return false
		default:
			// Other basic types (like string, unsafe pointer) are treated as reference types.
			// When GC-reachable in a slice's backing buffer (past len and within cap), they can keep objects alive.
			return true
		}
	case *types.Pointer:
		return true
	case *types.Interface:
		return true
	case *types.Slice:
		return true
	case *types.Map:
		return true
	case *types.Chan:
		return true
	case *types.Signature:
		return true
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if isOrContainsReferenceTypes(t.Field(i).Type()) {
				return true
			}
		}
		return false
	case *types.Array:
		return isOrContainsReferenceTypes(t.Elem())
	case *types.Named:
		return isOrContainsReferenceTypes(t.Underlying())
	default:
		return false
	}
}
