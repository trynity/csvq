package query

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mithrandie/csvq/lib/parser"
	"github.com/mithrandie/csvq/lib/ternary"
)

type FilterRecord struct {
	View        *View
	RecordIndex int
}

type Filter []FilterRecord

func (f Filter) Evaluate(expr parser.Expression) (parser.Primary, error) {
	if expr == nil {
		return parser.NewTernary(ternary.TRUE), nil
	}

	var primary parser.Primary
	var err error

	if parser.IsPrimary(expr) {
		primary = expr.(parser.Primary)
	} else {
		switch expr.(type) {
		case parser.Parentheses:
			primary, err = f.Evaluate(expr.(parser.Parentheses).Expr)
		case parser.FieldReference:
			primary, err = f.evalFieldReference(expr.(parser.FieldReference))
		case parser.Arithmetic:
			primary, err = f.evalArithmetic(expr.(parser.Arithmetic))
		case parser.Concat:
			primary, err = f.evalConcat(expr.(parser.Concat))
		case parser.Comparison:
			primary, err = f.evalComparison(expr.(parser.Comparison))
		case parser.Is:
			primary, err = f.evalIs(expr.(parser.Is))
		case parser.Between:
			primary, err = f.evalBetween(expr.(parser.Between))
		case parser.In:
			primary, err = f.evalIn(expr.(parser.In))
		case parser.Like:
			primary, err = f.evalLike(expr.(parser.Like))
		case parser.Any:
			primary, err = f.evalAny(expr.(parser.Any))
		case parser.All:
			primary, err = f.evalAll(expr.(parser.All))
		case parser.Exists:
			primary, err = f.evalExists(expr.(parser.Exists))
		case parser.Subquery:
			primary, err = f.evalSubquery(expr.(parser.Subquery))
		case parser.Function:
			primary, err = f.evalFunction(expr.(parser.Function))
		case parser.GroupConcat:
			primary, err = f.evalGroupConcat(expr.(parser.GroupConcat))
		case parser.Case:
			primary, err = f.evalCase(expr.(parser.Case))
		case parser.Logic:
			primary, err = f.evalLogic(expr.(parser.Logic))
		case parser.Variable:
			primary, err = f.evalVariable(expr.(parser.Variable))
		case parser.VariableSubstitution:
			primary, err = f.evalVariableSubstitution(expr.(parser.VariableSubstitution))
		default:
			return nil, errors.New(fmt.Sprintf("syntax error: unexpected %s", expr))
		}
	}

	return primary, err
}

func (f Filter) evalFieldReference(expr parser.FieldReference) (parser.Primary, error) {
	var p parser.Primary
	for _, v := range f {
		idx, err := v.View.Header.Contains(expr)
		if err != nil {
			switch err.(type) {
			case *IdentificationError:
				e, _ := err.(*IdentificationError)
				if e.Err == ErrFieldAmbiguous {
					return nil, err
				}
			}
			continue
		}
		if p != nil {
			return nil, &IdentificationError{
				Field: expr,
				Err:   ErrFieldAmbiguous,
			}
		}
		if v.View.isGrouped && !v.View.Header[idx].IsGroupKey {
			return nil, errors.New(fmt.Sprintf("field %s is not a group key", expr))
		}
		p = v.View.Records[v.RecordIndex][idx].Primary()
	}
	if p == nil {
		return nil, &IdentificationError{
			Field: expr,
			Err:   ErrFieldNotExist,
		}
	}
	return p, nil
}

func (f Filter) evalArithmetic(expr parser.Arithmetic) (parser.Primary, error) {
	lhs, err := f.Evaluate(expr.LHS)
	if err != nil {
		return nil, err
	}
	rhs, err := f.Evaluate(expr.RHS)
	if err != nil {
		return nil, err
	}

	return Calculate(lhs, rhs, expr.Operator), nil
}

func (f Filter) evalConcat(expr parser.Concat) (parser.Primary, error) {
	items := make([]string, len(expr.Items))
	for i, v := range expr.Items {
		s, err := f.Evaluate(v)
		if err != nil {
			return nil, err
		}
		s = parser.PrimaryToString(s)
		if parser.IsNull(s) {
			return parser.NewNull(), nil
		}
		items[i] = s.(parser.String).Value()
	}
	return parser.NewString(strings.Join(items, "")), nil
}

func (f Filter) evalComparison(expr parser.Comparison) (parser.Primary, error) {
	var t ternary.Value

	switch expr.LHS.(type) {
	case parser.RowValue:
		lhs, err := f.evalRowValue(expr.LHS.(parser.RowValue))
		if err != nil {
			return nil, err
		}

		rhs, err := f.evalRowValue(expr.RHS.(parser.RowValue))
		if err != nil {
			return nil, err
		}

		t, err = CompareRowValues(lhs, rhs, expr.Operator.Literal)
		if err != nil {
			return nil, err
		}

	default:
		lhs, err := f.Evaluate(expr.LHS)
		if err != nil {
			return nil, err
		}
		rhs, err := f.Evaluate(expr.RHS)
		if err != nil {
			return nil, err
		}

		t = Compare(lhs, rhs, expr.Operator.Literal)
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalIs(expr parser.Is) (parser.Primary, error) {
	lhs, err := f.Evaluate(expr.LHS)
	if err != nil {
		return nil, err
	}
	rhs, err := f.Evaluate(expr.RHS)
	if err != nil {
		return nil, err
	}

	t := Is(lhs, rhs)
	if expr.IsNegated() {
		t = ternary.Not(t)
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalBetween(expr parser.Between) (parser.Primary, error) {
	var t ternary.Value

	switch expr.LHS.(type) {
	case parser.RowValue:
		lhs, err := f.evalRowValue(expr.LHS.(parser.RowValue))
		if err != nil {
			return nil, err
		}

		low, err := f.evalRowValue(expr.Low.(parser.RowValue))
		if err != nil {
			return nil, err
		}

		high, err := f.evalRowValue(expr.High.(parser.RowValue))
		if err != nil {
			return nil, err
		}

		t1, err := CompareRowValues(lhs, low, ">=")
		if err != nil {
			return nil, err
		}
		t2, err := CompareRowValues(lhs, high, "<=")
		if err != nil {
			return nil, err
		}

		t = ternary.And(t1, t2)
	default:
		lhs, err := f.Evaluate(expr.LHS)
		if err != nil {
			return nil, err
		}
		low, err := f.Evaluate(expr.Low)
		if err != nil {
			return nil, err
		}
		high, err := f.Evaluate(expr.High)
		if err != nil {
			return nil, err
		}

		t = ternary.And(GreaterThanOrEqualTo(lhs, low), LessThanOrEqualTo(lhs, high))
	}

	if expr.IsNegated() {
		t = ternary.Not(t)
	}
	return parser.NewTernary(t), nil
}

func (f Filter) valuesForRowValueListComparison(lhs parser.Expression, values parser.Expression) ([]parser.Primary, [][]parser.Primary, error) {
	var value []parser.Primary
	var list [][]parser.Primary
	var err error

	switch lhs.(type) {
	case parser.RowValue:
		value, err = f.evalRowValue(lhs.(parser.RowValue))
		if err != nil {
			return nil, nil, err
		}

		list, err = f.evalRowValues(values)
		if err != nil {
			return nil, nil, err
		}

	default:
		lhs, err := f.Evaluate(lhs)
		if err != nil {
			return nil, nil, err
		}
		value = []parser.Primary{lhs}

		rowValue := values.(parser.RowValue)
		switch rowValue.Value.(type) {
		case parser.Subquery:
			list, err = f.evalSubqueryForRowValues(rowValue.Value.(parser.Subquery))
			if err != nil {
				return nil, nil, err
			}
		case parser.ValueList:
			values, err := f.evalValueList(rowValue.Value.(parser.ValueList))
			if err != nil {
				return nil, nil, err
			}
			list = make([][]parser.Primary, len(values))
			for i, v := range values {
				list[i] = []parser.Primary{v}
			}
		}
	}
	return value, list, nil
}

func (f Filter) evalIn(expr parser.In) (parser.Primary, error) {
	value, list, err := f.valuesForRowValueListComparison(expr.LHS, expr.Values)
	if err != nil {
		return nil, err
	}

	t, err := Any(value, list, "=")
	if err != nil {
		return nil, err
	}

	if expr.IsNegated() {
		t = ternary.Not(t)
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalAny(expr parser.Any) (parser.Primary, error) {
	value, list, err := f.valuesForRowValueListComparison(expr.LHS, expr.Values)
	if err != nil {
		return nil, err
	}

	t, err := Any(value, list, expr.Operator.Literal)
	if err != nil {
		return nil, err
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalAll(expr parser.All) (parser.Primary, error) {
	value, list, err := f.valuesForRowValueListComparison(expr.LHS, expr.Values)
	if err != nil {
		return nil, err
	}

	t, err := All(value, list, expr.Operator.Literal)
	if err != nil {
		return nil, err
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalLike(expr parser.Like) (parser.Primary, error) {
	lhs, err := f.Evaluate(expr.LHS)
	if err != nil {
		return nil, err
	}
	pattern, err := f.Evaluate(expr.Pattern)
	if err != nil {
		return nil, err
	}

	t := Like(lhs, pattern)
	if expr.IsNegated() {
		t = ternary.Not(t)
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalExists(expr parser.Exists) (parser.Primary, error) {
	view, err := Select(expr.Query.Query, f)
	if err != nil {
		return nil, err
	}
	if view.RecordLen() < 1 {
		return parser.NewTernary(ternary.FALSE), nil
	}
	return parser.NewTernary(ternary.TRUE), nil
}

func (f Filter) evalSubquery(expr parser.Subquery) (parser.Primary, error) {
	return f.evalSubqueryForSingleValue(expr.Query)
}

func (f Filter) evalFunction(expr parser.Function) (parser.Primary, error) {
	name := strings.ToUpper(expr.Name)
	if _, ok := AggregateFunctions[name]; ok {
		return f.evalAggregateFunction(expr)
	}

	fn, ok := Functions[name]
	if !ok {
		return nil, errors.New(fmt.Sprintf("function %s does not exist", expr.Name))
	}

	if expr.Option.IsDistinct() {
		return nil, errors.New(fmt.Sprintf("syntax error: unexpected %s", expr.Option.Distinct.Literal))
	}

	args := make([]parser.Primary, len(expr.Option.Args))
	for i, v := range expr.Option.Args {
		arg, err := f.Evaluate(v)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}

	return fn(args)
}

func (f Filter) evalAggregateFunction(expr parser.Function) (parser.Primary, error) {
	if !f[0].View.isGrouped {
		return nil, &NotGroupedError{
			Function: expr.Name,
			Err:      ErrNotGrouped,
		}
	}

	if len(expr.Option.Args) < 1 {
		return nil, errors.New(fmt.Sprintf("function %s requires 1 argument", expr.Name))
	} else if 1 < len(expr.Option.Args) {
		return nil, errors.New(fmt.Sprintf("function %s has too many arguments", expr.Name))
	}

	arg := expr.Option.Args[0]
	if _, ok := arg.(parser.AllColumns); ok {
		if !strings.EqualFold(expr.Name, "COUNT") {
			return nil, errors.New(fmt.Sprintf("syntax error: %s", expr))
		}
		arg = parser.NewInteger(1)
	}

	fr := f[0]
	view := NewViewFromGroupedRecord(fr)

	list := make([]parser.Primary, view.RecordLen())
	for i := 0; i < view.RecordLen(); i++ {
		var filter Filter = []FilterRecord{{View: view, RecordIndex: i}}
		p, err := filter.Evaluate(arg)
		if err != nil {
			if _, ok := err.(*NotGroupedError); ok {
				err = errors.New(fmt.Sprintf("syntax error: %s", expr))
			}
			return nil, err
		}
		list[i] = p
	}

	name := strings.ToUpper(expr.Name)
	fn, _ := AggregateFunctions[name]
	return fn(expr.Option.IsDistinct(), list), nil
}

func (f Filter) evalGroupConcat(expr parser.GroupConcat) (parser.Primary, error) {
	if !f[0].View.isGrouped {
		return nil, &NotGroupedError{
			Function: expr.GroupConcat,
			Err:      ErrNotGrouped,
		}
	}

	if len(expr.Option.Args) != 1 {
		return nil, errors.New(fmt.Sprintf("function %s takes 1 argument", expr.GroupConcat))
	}

	arg := expr.Option.Args[0]
	if _, ok := arg.(parser.AllColumns); ok {
		return nil, errors.New(fmt.Sprintf("syntax error: %s", expr))
	}

	fr := f[0]
	view := NewViewFromGroupedRecord(fr)
	if expr.OrderBy != nil {
		err := view.OrderBy(expr.OrderBy.(parser.OrderByClause))
		if err != nil {
			return nil, err
		}
	}

	list := []string{}
	for i := 0; i < view.RecordLen(); i++ {
		var filter Filter = []FilterRecord{{View: view, RecordIndex: i}}
		p, err := filter.Evaluate(arg)
		if err != nil {
			if _, ok := err.(*NotGroupedError); ok {
				err = errors.New(fmt.Sprintf("syntax error: %s", expr))
			}
			return nil, err
		}
		s := parser.PrimaryToString(p)
		if parser.IsNull(s) {
			continue
		}
		if expr.Option.IsDistinct() && InStrSlice(s.(parser.String).Value(), list) {
			continue
		}
		list = append(list, s.(parser.String).Value())
	}

	if len(list) < 1 {
		return parser.NewNull(), nil
	}
	return parser.NewString(strings.Join(list, expr.Separator)), nil
}

func (f Filter) evalCase(expr parser.Case) (parser.Primary, error) {
	var value parser.Primary
	var err error
	if expr.Value != nil {
		value, err = f.Evaluate(expr.Value)
		if err != nil {
			return nil, err
		}
	}

	for _, v := range expr.When {
		when := v.(parser.CaseWhen)
		var t ternary.Value

		cond, err := f.Evaluate(when.Condition)
		if err != nil {
			return nil, err
		}

		if value == nil {
			t = cond.Ternary()
		} else {
			t = EqualTo(value, cond)
		}

		if t == ternary.TRUE {
			result, err := f.Evaluate(when.Result)
			if err != nil {
				return nil, err
			}
			return result, nil
		}
	}

	if expr.Else == nil {
		return parser.NewNull(), nil
	}
	result, err := f.Evaluate(expr.Else.(parser.CaseElse).Result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (f Filter) evalLogic(expr parser.Logic) (parser.Primary, error) {
	lhs, err := f.Evaluate(expr.LHS)
	if err != nil {
		return nil, err
	}
	rhs, err := f.Evaluate(expr.RHS)
	if err != nil {
		return nil, err
	}

	var t ternary.Value
	switch expr.Operator.Token {
	case parser.AND:
		t = ternary.And(lhs.Ternary(), rhs.Ternary())
	case parser.OR:
		t = ternary.Or(lhs.Ternary(), rhs.Ternary())
	case parser.NOT:
		t = ternary.Not(rhs.Ternary())
	}
	return parser.NewTernary(t), nil
}

func (f Filter) evalVariable(expr parser.Variable) (parser.Primary, error) {
	return GlobalVars.Get(expr.Name)
}

func (f Filter) evalVariableSubstitution(expr parser.VariableSubstitution) (parser.Primary, error) {
	return GlobalVars.Substitute(expr, f)
}

func (f Filter) evalRowValue(expr parser.RowValue) (values []parser.Primary, err error) {
	switch expr.Value.(type) {
	case parser.Subquery:
		values, err = f.evalSubqueryForRowValue(expr.Value.(parser.Subquery))
	case parser.ValueList:
		values, err = f.evalValueList(expr.Value.(parser.ValueList))
	}

	return
}

func (f Filter) evalValues(exprs []parser.Expression) ([]parser.Primary, error) {
	values := make([]parser.Primary, len(exprs))
	for i, v := range exprs {
		value, err := f.Evaluate(v)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func (f Filter) evalValueList(expr parser.ValueList) ([]parser.Primary, error) {
	return f.evalValues(expr.Values)
}

func (f Filter) evalRowValueList(expr parser.RowValueList) ([][]parser.Primary, error) {
	list := make([][]parser.Primary, len(expr.RowValues))
	for i, v := range expr.RowValues {
		values, err := f.evalRowValue(v.(parser.RowValue))
		if err != nil {
			return nil, err
		}
		list[i] = values
	}
	return list, nil
}

func (f Filter) evalRowValues(expr parser.Expression) (values [][]parser.Primary, err error) {
	switch expr.(type) {
	case parser.Subquery:
		values, err = f.evalSubqueryForRowValues(expr.(parser.Subquery))
	case parser.RowValueList:
		values, err = f.evalRowValueList(expr.(parser.RowValueList))
	}

	return
}

func (f Filter) evalSubqueryForRowValue(expr parser.Subquery) ([]parser.Primary, error) {
	view, err := Select(expr.Query, f)
	if err != nil {
		return nil, err
	}

	if view.RecordLen() < 1 {
		return nil, nil
	}

	if 1 < view.RecordLen() {
		return nil, errors.New("subquery returns too many records, should be only one record")
	}

	values := make([]parser.Primary, view.FieldLen())
	for i, cell := range view.Records[0] {
		values[i] = cell.Primary()
	}

	return values, nil
}

func (f Filter) evalSubqueryForRowValues(expr parser.Subquery) ([][]parser.Primary, error) {
	view, err := Select(expr.Query, f)
	if err != nil {
		return nil, err
	}

	if view.RecordLen() < 1 {
		return nil, nil
	}

	list := make([][]parser.Primary, view.RecordLen())
	for i, r := range view.Records {
		values := make([]parser.Primary, view.FieldLen())
		for j, cell := range r {
			values[j] = cell.Primary()
		}
		list[i] = values
	}

	return list, nil
}

func (f Filter) evalSubqueryForSingleValue(query parser.SelectQuery) (parser.Primary, error) {
	view, err := Select(query, f)
	if err != nil {
		return nil, err
	}

	if 1 < view.FieldLen() {
		return nil, errors.New("subquery returns too many fields, should be only one field")
	}

	if 1 < view.RecordLen() {
		return nil, errors.New("subquery returns too many records, should be only one record")
	}

	if view.RecordLen() < 1 {
		return parser.NewNull(), nil
	}

	return view.Records[0][0].Primary(), nil
}
