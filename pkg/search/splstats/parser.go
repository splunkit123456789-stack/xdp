package splstats

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	MaxQueryLength = 512
	MaxAggregates  = 5
	MaxGroupBy     = 5
)

type Query struct {
	Raw        string
	Aggregates []Aggregate
	GroupBy    []FieldRef
}

type Aggregate struct {
	Func  string
	Field *FieldRef
	Alias string
}

type FieldRef struct {
	Scope string
	Name  string
}

type Result struct {
	Query  string           `json:"query"`
	Fields []string         `json:"fields"`
	Rows   []map[string]any `json:"rows"`
	Limit  int              `json:"limit"`
}

func Parse(input string) (Query, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Query{}, fmt.Errorf("stats query is required")
	}
	if len(raw) > MaxQueryLength {
		return Query{}, fmt.Errorf("stats query is too long")
	}
	if strings.HasPrefix(raw, "|") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "|"))
	}
	if strings.Contains(raw, "|") {
		return Query{}, fmt.Errorf("only one stats command is supported")
	}
	for _, r := range raw {
		if isDangerous(r) {
			return Query{}, fmt.Errorf("unsupported character %q", r)
		}
	}

	p := parser{tokens: tokenize(raw)}
	query := Query{Raw: strings.TrimSpace(input)}
	if !p.consumeKeyword("stats") {
		return Query{}, fmt.Errorf("only stats command is supported")
	}
	aggs, err := p.parseAggregates()
	if err != nil {
		return Query{}, err
	}
	query.Aggregates = aggs
	if p.consumeKeyword("by") {
		fields, err := p.parseFields()
		if err != nil {
			return Query{}, err
		}
		query.GroupBy = fields
	}
	if p.hasMore() {
		return Query{}, fmt.Errorf("unexpected token %q", p.peek())
	}
	if err := query.Validate(); err != nil {
		return Query{}, err
	}
	return query, nil
}

func (q Query) Validate() error {
	if len(q.Aggregates) == 0 {
		return fmt.Errorf("stats requires at least one aggregate")
	}
	if len(q.Aggregates) > MaxAggregates {
		return fmt.Errorf("too many aggregate functions")
	}
	if len(q.GroupBy) > MaxGroupBy {
		return fmt.Errorf("too many group by fields")
	}
	for _, agg := range q.Aggregates {
		if !supportedFunc(agg.Func) {
			return fmt.Errorf("unsupported stats function: %s", agg.Func)
		}
		if agg.Func != "count" && agg.Field == nil {
			return fmt.Errorf("%s requires a field", agg.Func)
		}
		if agg.Field != nil {
			if err := agg.Field.Validate(); err != nil {
				return err
			}
		}
		if agg.Alias != "" && !isIdent(agg.Alias) {
			return fmt.Errorf("invalid alias: %s", agg.Alias)
		}
	}
	for _, field := range q.GroupBy {
		if err := field.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (f FieldRef) Validate() error {
	if f.Name == "" || !isIdent(f.Name) {
		return fmt.Errorf("invalid field: %s", f.DisplayName())
	}
	switch f.Scope {
	case "", "fields":
		return nil
	case "metadata":
		switch f.Name {
		case "index", "sourcetype", "vendor", "product":
			return nil
		}
	case "source":
		switch f.Name {
		case "type", "name", "host", "ip":
			return nil
		}
	case "root":
		switch f.Name {
		case "index", "raw_length":
			return nil
		}
	}
	return fmt.Errorf("unsupported field: %s", f.DisplayName())
}

func (f FieldRef) DisplayName() string {
	if f.Scope == "" || f.Scope == "fields" {
		return f.Name
	}
	if f.Scope == "root" {
		return f.Name
	}
	return f.Scope + "." + f.Name
}

func (a Aggregate) DisplayName() string {
	if a.Alias != "" {
		return a.Alias
	}
	if a.Func == "count" && a.Field == nil {
		return "count"
	}
	if a.Field != nil {
		return a.Func + "_" + strings.ReplaceAll(a.Field.DisplayName(), ".", "_")
	}
	return a.Func
}

type parser struct {
	tokens []string
	pos    int
}

func (p *parser) parseAggregates() ([]Aggregate, error) {
	aggs := []Aggregate{}
	for {
		agg, err := p.parseAggregate()
		if err != nil {
			return nil, err
		}
		aggs = append(aggs, agg)
		if p.consume(",") {
			continue
		}
		if !p.hasMore() || strings.EqualFold(p.peek(), "by") {
			break
		}
	}
	return aggs, nil
}

func (p *parser) parseAggregate() (Aggregate, error) {
	fn := strings.ToLower(p.next())
	if fn == "" {
		return Aggregate{}, fmt.Errorf("stats aggregate is required")
	}
	if !supportedFunc(fn) {
		return Aggregate{}, fmt.Errorf("unsupported stats function: %s", fn)
	}
	agg := Aggregate{Func: fn}
	if p.consume("(") {
		if !p.consume(")") {
			field, err := p.parseField()
			if err != nil {
				return Aggregate{}, err
			}
			agg.Field = &field
			if !p.consume(")") {
				return Aggregate{}, fmt.Errorf("missing closing parenthesis")
			}
		}
	}
	if agg.Func != "count" && agg.Field == nil {
		return Aggregate{}, fmt.Errorf("%s requires a field", agg.Func)
	}
	if p.consumeKeyword("as") {
		alias := p.next()
		if !isIdent(alias) {
			return Aggregate{}, fmt.Errorf("invalid alias: %s", alias)
		}
		agg.Alias = alias
	}
	return agg, nil
}

func (p *parser) parseFields() ([]FieldRef, error) {
	fields := []FieldRef{}
	for {
		field, err := p.parseField()
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
		if p.consume(",") {
			continue
		}
		if !p.hasMore() {
			break
		}
	}
	return fields, nil
}

func (p *parser) parseField() (FieldRef, error) {
	first := p.next()
	if !isIdent(first) {
		return FieldRef{}, fmt.Errorf("invalid field: %s", first)
	}
	if p.consume(".") {
		second := p.next()
		if !isIdent(second) {
			return FieldRef{}, fmt.Errorf("invalid field: %s.%s", first, second)
		}
		return scopedField(first, second), nil
	}
	return scopedField("", first), nil
}

func scopedField(scope, name string) FieldRef {
	scope = strings.ToLower(scope)
	name = strings.ToLower(name)
	if scope == "" {
		switch name {
		case "index", "raw_length":
			return FieldRef{Scope: "root", Name: name}
		default:
			return FieldRef{Name: name}
		}
	}
	return FieldRef{Scope: scope, Name: name}
}

func (p *parser) consumeKeyword(value string) bool {
	if strings.EqualFold(p.peek(), value) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) consume(value string) bool {
	if p.peek() == value {
		p.pos++
		return true
	}
	return false
}

func (p *parser) next() string {
	if !p.hasMore() {
		return ""
	}
	value := p.tokens[p.pos]
	p.pos++
	return value
}

func (p *parser) peek() string {
	if !p.hasMore() {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *parser) hasMore() bool { return p.pos < len(p.tokens) }

func tokenize(input string) []string {
	tokens := []string{}
	for i := 0; i < len(input); {
		ch := rune(input[i])
		if unicode.IsSpace(ch) {
			i++
			continue
		}
		if strings.ContainsRune(",().", ch) {
			tokens = append(tokens, string(ch))
			i++
			continue
		}
		start := i
		for i < len(input) {
			r := rune(input[i])
			if unicode.IsSpace(r) || strings.ContainsRune(",().", r) {
				break
			}
			i++
		}
		tokens = append(tokens, input[start:i])
	}
	return tokens
}

func supportedFunc(fn string) bool {
	switch fn {
	case "count", "sum", "avg", "min", "max":
		return true
	default:
		return false
	}
}

func isIdent(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isDangerous(r rune) bool {
	switch r {
	case ';', '\'', '"', '*', '=', '<', '>', '[', ']', '{', '}':
		return true
	default:
		return false
	}
}
