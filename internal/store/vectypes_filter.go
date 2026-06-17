package store

// Native request structs and payload-filter DTOs (companion to vectypes.go /
// vectypes_request.go). The request structs carry exactly the fields the SQLite
// store reads plus the opaque pass-through fields (OrderBy/ReadConsistency/
// ShardKeySelector/Timeout/Wait) that callers copy between requests but the
// embedded backend ignores. The opaque fields are typed `any` because nothing in
// this package ever inspects them.

// --- collection lifecycle requests ---

// CreateCollection requests creation of a collection with a dense vector config.
type CreateCollection struct {
	CollectionName string
	VectorsConfig  *VectorsConfig
}

func (r *CreateCollection) GetVectorsConfig() *VectorsConfig {
	if r == nil {
		return nil
	}
	return r.VectorsConfig
}

// --- write requests ---

// UpsertPoints inserts or replaces points in a collection.
type UpsertPoints struct {
	CollectionName string
	Wait           *bool
	Points         []*PointStruct
}

func (r *UpsertPoints) GetPoints() []*PointStruct {
	if r == nil {
		return nil
	}
	return r.Points
}

// SetPayloadPoints merges payload onto the selected points (partial update).
type SetPayloadPoints struct {
	CollectionName   string
	Wait             *bool
	Payload          map[string]*Value
	PointsSelector   *PointsSelector
	ShardKeySelector any
}

func (r *SetPayloadPoints) GetPayload() map[string]*Value {
	if r == nil {
		return nil
	}
	return r.Payload
}

func (r *SetPayloadPoints) GetPointsSelector() *PointsSelector {
	if r == nil {
		return nil
	}
	return r.PointsSelector
}

// DeletePoints removes the selected points from a collection.
type DeletePoints struct {
	CollectionName string
	Wait           *bool
	Points         *PointsSelector
}

func (r *DeletePoints) GetPoints() *PointsSelector {
	if r == nil {
		return nil
	}
	return r.Points
}

// --- read requests ---

// QueryPoints runs a nearest-neighbor query with optional filter/projection.
type QueryPoints struct {
	CollectionName string
	Query          *Query
	Filter         *Filter
	Limit          *uint64
	ScoreThreshold *float32
	WithPayload    *WithPayloadSelector
	WithVectors    *WithVectorsSelector
}

func (r *QueryPoints) GetQuery() *Query {
	if r == nil {
		return nil
	}
	return r.Query
}

func (r *QueryPoints) GetFilter() *Filter {
	if r == nil {
		return nil
	}
	return r.Filter
}

func (r *QueryPoints) GetLimit() uint64 {
	if r == nil || r.Limit == nil {
		return 0
	}
	return *r.Limit
}

func (r *QueryPoints) GetScoreThreshold() float32 {
	if r == nil || r.ScoreThreshold == nil {
		return 0
	}
	return *r.ScoreThreshold
}

func (r *QueryPoints) GetWithPayload() *WithPayloadSelector {
	if r == nil {
		return nil
	}
	return r.WithPayload
}

func (r *QueryPoints) GetWithVectors() *WithVectorsSelector {
	if r == nil {
		return nil
	}
	return r.WithVectors
}

// GetPoints fetches points by explicit ids with optional projection.
type GetPoints struct {
	CollectionName string
	Ids            []*PointId
	WithPayload    *WithPayloadSelector
	WithVectors    *WithVectorsSelector
}

func (r *GetPoints) GetIds() []*PointId {
	if r == nil {
		return nil
	}
	return r.Ids
}

func (r *GetPoints) GetWithPayload() *WithPayloadSelector {
	if r == nil {
		return nil
	}
	return r.WithPayload
}

func (r *GetPoints) GetWithVectors() *WithVectorsSelector {
	if r == nil {
		return nil
	}
	return r.WithVectors
}

// ScrollPoints paginates points ordered by id, with optional filter/projection.
// OrderBy/ReadConsistency/ShardKeySelector/Timeout are opaque pass-throughs the
// embedded store ignores.
type ScrollPoints struct {
	CollectionName   string
	Filter           *Filter
	Limit            *uint32
	Offset           *PointId
	WithPayload      *WithPayloadSelector
	WithVectors      *WithVectorsSelector
	OrderBy          any
	ReadConsistency  any
	ShardKeySelector any
	Timeout          any
}

func (r *ScrollPoints) GetFilter() *Filter {
	if r == nil {
		return nil
	}
	return r.Filter
}

func (r *ScrollPoints) GetWithPayload() *WithPayloadSelector {
	if r == nil {
		return nil
	}
	return r.WithPayload
}

func (r *ScrollPoints) GetWithVectors() *WithVectorsSelector {
	if r == nil {
		return nil
	}
	return r.WithVectors
}

// CountPoints counts points in a collection matching an optional filter.
type CountPoints struct {
	CollectionName string
	Filter         *Filter
}

func (r *CountPoints) GetFilter() *Filter {
	if r == nil {
		return nil
	}
	return r.Filter
}

// --- payload filter (Filter / Condition / Match shapes) ---

// Filter is a boolean combination of conditions: Must (AND), MustNot (NOT),
// Should (OR when present).
type Filter struct {
	Must    []*Condition
	MustNot []*Condition
	Should  []*Condition
}

func (f *Filter) GetMust() []*Condition {
	if f == nil {
		return nil
	}
	return f.Must
}

func (f *Filter) GetMustNot() []*Condition {
	if f == nil {
		return nil
	}
	return f.MustNot
}

func (f *Filter) GetShould() []*Condition {
	if f == nil {
		return nil
	}
	return f.Should
}

// Condition is one filter clause; exactly one ConditionOneOf variant is set.
type Condition struct {
	ConditionOneOf isConditionOneOf
}

type isConditionOneOf interface{ isConditionOneOf() }

type Condition_Field struct{ Field *FieldCondition }
type Condition_Filter struct{ Filter *Filter }
type Condition_IsEmpty struct{ IsEmpty *IsEmptyCondition }
type Condition_IsNull struct{ IsNull *IsNullCondition }

func (*Condition_Field) isConditionOneOf()   {}
func (*Condition_Filter) isConditionOneOf()  {}
func (*Condition_IsEmpty) isConditionOneOf() {}
func (*Condition_IsNull) isConditionOneOf()  {}

func (c *Condition) GetConditionOneOf() isConditionOneOf {
	if c == nil {
		return nil
	}
	return c.ConditionOneOf
}

// GetField / GetFilter / GetIsEmpty / GetIsNull return the set variant payload,
// or nil when a different variant is set. Used by filter-introspection tests.
func (c *Condition) GetField() *FieldCondition {
	if c == nil {
		return nil
	}
	if v, ok := c.ConditionOneOf.(*Condition_Field); ok {
		return v.Field
	}
	return nil
}

func (c *Condition) GetFilter() *Filter {
	if c == nil {
		return nil
	}
	if v, ok := c.ConditionOneOf.(*Condition_Filter); ok {
		return v.Filter
	}
	return nil
}

func (c *Condition) GetIsEmpty() *IsEmptyCondition {
	if c == nil {
		return nil
	}
	if v, ok := c.ConditionOneOf.(*Condition_IsEmpty); ok {
		return v.IsEmpty
	}
	return nil
}

func (c *Condition) GetIsNull() *IsNullCondition {
	if c == nil {
		return nil
	}
	if v, ok := c.ConditionOneOf.(*Condition_IsNull); ok {
		return v.IsNull
	}
	return nil
}

// IsEmptyCondition / IsNullCondition target a single key.
type IsEmptyCondition struct{ Key string }
type IsNullCondition struct{ Key string }

func (c *IsEmptyCondition) GetKey() string {
	if c == nil {
		return ""
	}
	return c.Key
}

func (c *IsNullCondition) GetKey() string {
	if c == nil {
		return ""
	}
	return c.Key
}

// FieldCondition matches a payload field against a Match value.
type FieldCondition struct {
	Key   string
	Match *Match
}

func (c *FieldCondition) GetKey() string {
	if c == nil {
		return ""
	}
	return c.Key
}

func (c *FieldCondition) GetMatch() *Match {
	if c == nil {
		return nil
	}
	return c.Match
}

// Match is the matched-value oneof for a FieldCondition.
type Match struct {
	MatchValue isMatchValue
}

type isMatchValue interface{ isMatchValue() }

type Match_Keyword struct{ Keyword string }
type Match_Keywords struct{ Keywords *RepeatedStrings }
type Match_ExceptKeywords struct{ ExceptKeywords *RepeatedStrings }
type Match_Boolean struct{ Boolean bool }
type Match_Integer struct{ Integer int64 }
type Match_Integers struct{ Integers *RepeatedIntegers }

func (*Match_Keyword) isMatchValue()        {}
func (*Match_Keywords) isMatchValue()       {}
func (*Match_ExceptKeywords) isMatchValue() {}
func (*Match_Boolean) isMatchValue()        {}
func (*Match_Integer) isMatchValue()        {}
func (*Match_Integers) isMatchValue()       {}

func (m *Match) GetMatchValue() isMatchValue {
	if m == nil {
		return nil
	}
	return m.MatchValue
}

// GetKeyword / GetKeywords / GetExceptKeywords / GetBoolean / GetInteger /
// GetIntegers return the set match operand, or the zero value when a different
// variant is set. Used by filter-introspection tests.
func (m *Match) GetKeyword() string {
	if m == nil {
		return ""
	}
	if v, ok := m.MatchValue.(*Match_Keyword); ok {
		return v.Keyword
	}
	return ""
}

func (m *Match) GetKeywords() *RepeatedStrings {
	if m == nil {
		return nil
	}
	if v, ok := m.MatchValue.(*Match_Keywords); ok {
		return v.Keywords
	}
	return nil
}

func (m *Match) GetExceptKeywords() *RepeatedStrings {
	if m == nil {
		return nil
	}
	if v, ok := m.MatchValue.(*Match_ExceptKeywords); ok {
		return v.ExceptKeywords
	}
	return nil
}

func (m *Match) GetBoolean() bool {
	if m == nil {
		return false
	}
	if v, ok := m.MatchValue.(*Match_Boolean); ok {
		return v.Boolean
	}
	return false
}

func (m *Match) GetInteger() int64 {
	if m == nil {
		return 0
	}
	if v, ok := m.MatchValue.(*Match_Integer); ok {
		return v.Integer
	}
	return 0
}

func (m *Match) GetIntegers() *RepeatedIntegers {
	if m == nil {
		return nil
	}
	if v, ok := m.MatchValue.(*Match_Integers); ok {
		return v.Integers
	}
	return nil
}

// RepeatedStrings / RepeatedIntegers carry the multi-value match operands.
type RepeatedStrings struct{ Strings []string }
type RepeatedIntegers struct{ Integers []int64 }

func (r *RepeatedStrings) GetStrings() []string {
	if r == nil {
		return nil
	}
	return r.Strings
}

func (r *RepeatedIntegers) GetIntegers() []int64 {
	if r == nil {
		return nil
	}
	return r.Integers
}

// --- condition builders (NewMatch* / NewIsEmpty) ---

// NewMatchKeyword matches a field equal to a single keyword.
func NewMatchKeyword(field, keyword string) *Condition {
	return &Condition{ConditionOneOf: &Condition_Field{Field: &FieldCondition{
		Key:   field,
		Match: &Match{MatchValue: &Match_Keyword{Keyword: keyword}},
	}}}
}

// NewMatchKeywords matches a field equal to any of the keywords.
func NewMatchKeywords(field string, keywords ...string) *Condition {
	return &Condition{ConditionOneOf: &Condition_Field{Field: &FieldCondition{
		Key:   field,
		Match: &Match{MatchValue: &Match_Keywords{Keywords: &RepeatedStrings{Strings: keywords}}},
	}}}
}

// NewMatchBool matches a boolean field.
func NewMatchBool(field string, value bool) *Condition {
	return &Condition{ConditionOneOf: &Condition_Field{Field: &FieldCondition{
		Key:   field,
		Match: &Match{MatchValue: &Match_Boolean{Boolean: value}},
	}}}
}

// NewMatchInt matches an integer field.
func NewMatchInt(field string, value int64) *Condition {
	return &Condition{ConditionOneOf: &Condition_Field{Field: &FieldCondition{
		Key:   field,
		Match: &Match{MatchValue: &Match_Integer{Integer: value}},
	}}}
}

// NewIsEmpty matches when a field is absent, null, or an empty list.
func NewIsEmpty(field string) *Condition {
	return &Condition{ConditionOneOf: &Condition_IsEmpty{IsEmpty: &IsEmptyCondition{Key: field}}}
}
